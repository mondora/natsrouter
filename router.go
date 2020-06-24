package natsrouter

import (
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"regexp"
	"strings"
	"sync"
)

// Handle is a function that can be registered to a route to handle NATS
// requests. It has a third parameter for the values of wildcards (path variables).
type Handle func(*nats.Msg, Params, interface{})

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

var reNATSPathCatchAll = regexp.MustCompile(`(.*)\.>$`) // nolint
var reNATSPathToken = regexp.MustCompile(`(\.\*)`)      // nolint

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
	trees map[string]*node

	paramsPool sync.Pool
	maxParams  uint16

	// If enabled, adds the matched route path onto the request context
	// before invoking the handler.
	// The matched route path is only added to handlers of routes that were
	// registered when this option was enabled.
	SaveMatchedRoutePath bool

	// Cached value of global (*) allowed methods
	globalAllowed string

	// Function to handle panics recovered from NATS handlers.
	// The handler can be used to keep your server from crashing because of
	// unrecovered panics.
	PanicHandler func(*nats.Msg, interface{})
}

// New returns a new initialized Router.
// Path auto-correction, including trailing slashes, is enabled by default.
func New() *Router {
	return &Router{}
}

func (r *Router) getParams() *Params {
	ps := r.paramsPool.Get().(*Params)
	*ps = (*ps)[0:0] // reset slice
	return ps
}

func (r *Router) putParams(ps *Params) {
	if ps != nil {
		r.paramsPool.Put(ps)
	}
}

func (r *Router) saveMatchedRoutePath(path string, handle Handle) Handle {
	return func(msg *nats.Msg, ps Params, payload interface{}) {
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
func (r *Router) Handle(method, path string, handle Handle) {
	varsCount := uint16(0)

	if method == "" {
		panic("method must not be empty")
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
		r.trees = make(map[string]*node)
	}

	root := r.trees[method]
	if root == nil {
		root = new(node)
		r.trees[method] = root

		r.globalAllowed = r.allowed("*", "")
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

// Lookup allows the manual lookup of a method + path combo.
// This is e.g. useful to build a framework around this router.
// If the path was found, it returns the handle function and the path parameter
// values.
func (r *Router) Lookup(method, path string) (Handle, Params, bool) {
	if root := r.trees[method]; root != nil {
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

func (r *Router) allowed(path, reqMethod string) (allow string) {
	allowed := make([]string, 0, 9)

	if path == "*" { // server-wide
		// empty method is used for internal calls to refresh the cache
		if reqMethod == "" {
			for method := range r.trees {
				// Add request method to list of allowed methods
				allowed = append(allowed, method)
			}
		} else {
			return r.globalAllowed
		}
	} else { // specific path
		for method := range r.trees {
			// Skip the requested method - we already tried this one
			if method == reqMethod {
				continue
			}

			handle, _, _ := r.trees[method].getValue(path, nil)
			if handle != nil {
				// Add request method to list of allowed methods
				allowed = append(allowed, method)
			}
		}
	}

	if len(allowed) > 0 {
		// Sort allowed methods.
		// sort.Strings(allowed) unfortunately causes unnecessary allocations
		// due to allowed being moved to the heap and interface conversion
		for i, l := 1, len(allowed); i < l; i++ {
			for j := i; j > 0 && allowed[j] < allowed[j-1]; j-- {
				allowed[j], allowed[j-1] = allowed[j-1], allowed[j]
			}
		}

		// return as comma separated list
		return strings.Join(allowed, ", ")
	}
	return ""
}

func (r *Router) recv(msg *nats.Msg) {
	if rcv := recover(); rcv != nil {
		r.PanicHandler(msg, rcv)
	}
}

// ServeNATS makes the router implement interface.
func (r *Router) ServeNATS(msg *nats.Msg) error {
	if r.PanicHandler != nil {
		defer r.recv(msg)
	}

	path := msg.Subject

	if root := r.trees["SUB"]; root != nil {
		if handle, ps, _ := root.getValue(path, r.getParams); handle != nil {
			if ps != nil {
				handle(msg, *ps, nil)
				r.putParams(ps)
			} else {
				handle(msg, nil, nil)
			}
			return nil
		}
	}
	// Handle 404
	return errors.New("404 NotFound")
}

func (r *Router) ServeNATSWithPayload(msg *nats.Msg, payload interface{}) error {
	if r.PanicHandler != nil {
		defer r.recv(msg)
	}

	path := msg.Subject

	if root := r.trees["SUB"]; root != nil {
		if handle, ps, _ := root.getValue(path, r.getParams); handle != nil {
			if ps != nil {
				handle(msg, *ps, payload)
				r.putParams(ps)
			} else {
				handle(msg, nil, payload)
			}
			return nil
		}
	}
	// Handle 404
	return errors.New("404 NotFound")
}
