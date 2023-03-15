package natsrouter

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type SubjectMsg interface {
	GetMsg() interface{}
	GetSubject() string
}

// Handle is a function that can be registered to a route to handle NATS
// requests. It has a third parameter for the values of wildcards (path variables).
type Handle func(SubjectMsg, Params, interface{})

// Param is a single parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first TOPIC parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

// ByName returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) ByName(name string) string {
	for _, p := range ps {
		if p.Key == name {
			return p.Value
		}
	}

	return ""
}

var (
	reNATSPathCatchAll = regexp.MustCompile(`(.*)\.>$`)
	reNATSPathToken    = regexp.MustCompile(`(\.\*)`)
)

const subsNATSPath = "$1.*>"

func fromNatsPath(path string) string {
	i := 0
	path = reNATSPathToken.ReplaceAllStringFunc(path, func(string) string {
		i++

		return fmt.Sprintf(".:p%d", i)
	})
	result := reNATSPathCatchAll.ReplaceAllString(path, subsNATSPath)

	return result
}

// MatchedRoutePathParam is the Param name under which the path of the matched
// route is stored, if Router.SaveMatchedRoutePath is set.
var MatchedRoutePathParam = "$matchedRoutePath" //nolint

// MatchedRoutePath retrieves the path of the matched route.
// Router.SaveMatchedRoutePath must have been enabled when the respective
// handler was added, otherwise this function always returns an empty string.
func (ps Params) MatchedRoutePath() string {
	return ps.ByName(MatchedRoutePathParam)
}

// Router is a handler which can be used to dispatch requests to different
// handler functions via configurable routes
type Router struct {
	trees map[int]*node
	// rank map start from priority 1 to max 255

	paramsPool sync.Pool
	maxParams  uint16

	// If enabled, adds the matched route path onto the request context
	// before invoking the handler.
	// The matched route path is only added to handlers of routes that were
	// registered when this option was enabled.
	SaveMatchedRoutePath bool

	// Cached value of global (*) allowed ranks
	globalAllowed string

	// sorted rank list
	rankIndexList []int
	initialized   bool

	// Function to handle panics recovered from NATS handlers.
	// The handler can be used to keep your server from crashing because of
	// unrecovered panics.
	PanicHandler func(SubjectMsg, interface{})
}

// New returns a new initialized Router.
// Path auto-correction, including trailing slashes, is enabled by default.
func New() *Router {
	return &Router{
		initialized:   false,
		rankIndexList: make([]int, 0, 5),
	}
}

func (r *Router) getParams() *Params {
	if ps, ok := r.paramsPool.Get().(*Params); ok {
		*ps = (*ps)[0:0] // reset slice

		return ps
	}

	return nil
}

func (r *Router) putParams(ps *Params) {
	if ps != nil {
		r.paramsPool.Put(ps)
	}
}

func (r *Router) saveMatchedRoutePath(path string, handle Handle) Handle {
	return func(msg SubjectMsg, ps Params, payload interface{}) {
		if ps == nil {
			psp := r.getParams()
			ps := (*psp)[0:1]
			ps[0] = Param{Key: MatchedRoutePathParam, Value: path}
			handle(msg, ps, payload)
			r.putParams(psp)
		} else {
			ps = append(ps, Param{Key: MatchedRoutePathParam, Value: path})
			handle(msg, ps, payload)
		}
	}
}

// Handle registers a new request handle with the given path.
func (r *Router) Handle(path string, rank int, handle Handle) {
	varsCount := uint16(0)

	if rank <= 0 || rank > 255 {
		panic("rank must be > 0")
	}
	if handle == nil {
		panic("handle must not be nil")
	}
	path = fromNatsPath(path)

	if r.SaveMatchedRoutePath {
		varsCount++
		handle = r.saveMatchedRoutePath(path, handle)
	}

	if r.trees == nil {
		r.trees = make(map[int]*node)
	}

	root := r.trees[rank]
	if root == nil {
		root = new(node)
		r.trees[rank] = root

		r.globalAllowed = r.allowed("*", 0)
	}

	root.addRoute(path, handle)

	// Update maxParams
	if paramsCount := countParams(path); paramsCount+varsCount > r.maxParams {
		r.maxParams = paramsCount + varsCount
	}

	// Lazy-init paramsPool alloc func
	if r.paramsPool.New == nil && r.maxParams > 0 {
		r.paramsPool.New = func() interface{} {
			ps := make(Params, 0, r.maxParams)

			return &ps
		}
	}
}

// Lookup allows the manual lookup of a rank + path combo.
// This is e.g. useful to build a framework around this router.
// If the path was found, it returns the handle function and the path parameter
// values.
func (r *Router) Lookup(path string, rank int) (Handle, Params, bool) {
	if root := r.trees[rank]; root != nil {
		handle, ps, tsr := root.getValue(path, r.getParams)
		if handle == nil {
			r.putParams(ps)

			return nil, nil, tsr
		}
		if ps == nil {
			return handle, nil, tsr
		}

		return handle, *ps, tsr
	}

	return nil, nil, false
}

func (r *Router) allowed(path string, reqRank int) (allow string) {
	allowed := make([]int, 0, 9)

	if path == "*" { // server-wide
		// 0 rank is used for internal calls to refresh the cache
		if reqRank == 0 {
			for rank := range r.trees {
				// Add request rank to list of allowed ranks
				allowed = append(allowed, rank)
			}
		} else {
			return r.globalAllowed
		}
	} else { // specific path
		for rank := range r.trees {
			// Skip the requested rank - we already tried this one
			if rank == reqRank {
				continue
			}

			handle, _, _ := r.trees[rank].getValue(path, nil)
			if handle != nil {
				// Add request rank to list of allowed ranks
				allowed = append(allowed, rank)
			}
		}
	}

	if len(allowed) > 0 {
		// Sort allowed ranks.
		// sort.Strings(allowed) unfortunately causes unnecessary allocations
		// due to allowed being moved to the heap and interface conversion
		for i, l := 1, len(allowed); i < l; i++ {
			for j := i; j > 0 && allowed[j] < allowed[j-1]; j-- {
				allowed[j], allowed[j-1] = allowed[j-1], allowed[j]
			}
		}

		// return as comma separated list
		allowedStr := []string{}
		for i := range allowed {
			prio := allowed[i]
			ptxt := strconv.Itoa(prio)
			allowedStr = append(allowedStr, ptxt)
		}

		return strings.Join(allowedStr, ", ")
	}

	return ""
}

func (r *Router) recv(msg SubjectMsg) {
	if rcv := recover(); rcv != nil {
		r.PanicHandler(msg, rcv)
	}
}

func (r *Router) getRankList() []int {
	if !r.initialized {
		for rank := range r.trees {
			r.rankIndexList = append(r.rankIndexList, rank)
		}
		sort.Ints(r.rankIndexList)
		r.initialized = true
	}

	return r.rankIndexList
}

// ServeNATS makes the router implement interface.
func (r *Router) ServeNATS(msg SubjectMsg) error {
	if r.PanicHandler != nil {
		defer r.recv(msg)
	}

	path := msg.GetSubject()

	rankList := r.getRankList()
	for _, rank := range rankList {
		if root := r.trees[rank]; root != nil {
			if handle, ps, _ := root.getValue(path, r.getParams); handle != nil {
				if ps != nil {
					go func() {
						handle(msg, *ps, nil)
						r.putParams(ps)
					}()
				} else {
					go func() {
						handle(msg, nil, nil)
					}()
				}

				return nil
			}
		}
	}
	// Handle 404
	return errors.New("404 NotFound")
}

func (r *Router) ServeNATSWithPayload(msg SubjectMsg, payload interface{}) error {
	if r.PanicHandler != nil {
		defer r.recv(msg)
	}

	path := msg.GetSubject()

	rankList := r.getRankList()
	for _, rank := range rankList {
		if root := r.trees[rank]; root != nil {
			if handle, ps, _ := root.getValue(path, r.getParams); handle != nil {
				if ps != nil {
					go func() {
						handle(msg, *ps, payload)
						r.putParams(ps)
					}()
				} else {
					go func() {
						handle(msg, nil, payload)
					}()
				}

				return nil
			}
		}
	}
	// Handle 404
	return errors.New("404 NotFound")
}
