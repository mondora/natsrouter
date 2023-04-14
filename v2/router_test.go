package natsrouter

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	notAvailable = "N/A"
)

// NatsMsgFake simil to nats.Msg
type NatsMsgFake struct {
	Data    []byte
	Subject string
	Sub     *struct {
		Subject string
	}
}

type Msg struct {
	msg interface{}
	sub string
}

func (m *Msg) GetMsg() interface{} {
	return m.msg
}

func (m *Msg) GetSubject() string {
	return m.sub
}

func NewMessage(subject string) SubjectMsg {
	natsMsg := &NatsMsgFake{
		Subject: subject,
	}
	var msg SubjectMsg
	msg = &Msg{
		msg: natsMsg,
		sub: natsMsg.Subject,
	}

	return msg
}

func catchPanic(testFunc func()) (recv interface{}) {
	defer func() {
		recv = recover()
	}()

	testFunc()

	return
}

func TestPathCathAll(t *testing.T) {
	assert.Equal(t, "user.*>", fromNatsPath("user.>"))
	assert.Equal(t, "user.:p1.:p2.*>", fromNatsPath("user.*.*.>"))
}

func TestParams(t *testing.T) {
	ps := Params{
		Param{"param1", "value1"},
		Param{"param2", "value2"},
		Param{"param3", "value3"},
	}
	for i := range ps {
		if val := ps.ByName(ps[i].Key); val != ps[i].Value {
			t.Errorf("Wrong value for %s: Got %s; Want %s", ps[i].Key, val, ps[i].Value)
		}
	}
	if val := ps.ByName("noKey"); val != "" {
		t.Errorf("Expected empty string for not found key; got: %s", val)
	}
}

func TestRouter(t *testing.T) {
	router := New()

	var wg sync.WaitGroup
	wg.Add(1)
	routed := false
	router.Handle("user.:name", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		want := Params{Param{"name", "gopher"}}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})

	msg := NewMessage("user.gopher")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterCatchAll(t *testing.T) {
	router := New()

	var wg sync.WaitGroup
	wg.Add(1)
	routed := false
	router.Handle("user.*.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		// want := Params{Param{">", ".gopher.ok"}}
		want := Params{
			Param{"p1", "gopher"},
			Param{">", ".star.ok"},
		}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})

	msg := NewMessage("user.gopher.star.ok")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterMulti(t *testing.T) {
	router := New()
	var wg sync.WaitGroup
	wg.Add(1) // one request (2 handlers)
	routed := false
	router.Handle("confirm-subsbscription.:mongoid.:correlationid.:pincode.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		want := Params{
			Param{"mongoid", "1234"},
			Param{"correlationid", "5678"},
			Param{"pincode", "1111"},
		}
		if !reflect.DeepEqual(ps, want) {
			if ps[len(ps)-1].Key != `>` {
				t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
			}
		}
	})
	router.Handle("reset-subsbscription.:mongoid.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		want := Params{
			Param{"mongoid", "1234"},
		}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})
	msg := NewMessage("confirm-subsbscription.1234.5678.1111.>")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func getRoutingSubscription(subtopic string, star bool) string {
	var subject string
	if subtopic == "" {
		subject = "ROUTING.v2"
	} else {
		subject = fmt.Sprintf("%s.%s", "ROUTING.v2", subtopic)
	}
	if star {
		return subject + ".>"
	}

	return subject
}

func TestRouterMulti2(t *testing.T) {
	router := New()
	routed := false
	result := notAvailable
	var wg sync.WaitGroup
	wg.Add(1)
	router.Handle(getRoutingSubscription(":context", true), 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		assert.True(t, len(ps) > 0)
		assert.Equal(t, "context", ps[0].Key)
		assert.Equal(t, "HR", ps[0].Value)
		assert.Equal(t, ">", ps[1].Key)
		assert.Equal(t, ".AnagraficheDipendenti_Paghe_HRportal", ps[1].Value)
		result = msg.GetSubject() // getRoutingSubscription("*", true)
	})
	// TODO complete
	msg := NewMessage("ROUTING.v2.HR.AnagraficheDipendenti_Paghe_HRportal")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
	assert.Equal(t, "ROUTING.v2.HR.AnagraficheDipendenti_Paghe_HRportal", result)
}

func TestRouterMulti2b(t *testing.T) {
	router := New()
	routed := false
	result := notAvailable
	routingSubs := getRoutingSubscription("*", true)
	assert.Equal(t, "ROUTING.v2.*.>", routingSubs)
	var wg sync.WaitGroup
	wg.Add(1)
	router.Handle(getRoutingSubscription("*", true), 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		assert.True(t, len(ps) > 0)
		assert.Equal(t, "p1", ps[0].Key)
		assert.Equal(t, "BI", ps[0].Value)
		result = msg.GetSubject() // getRoutingSubscription("*", true)
	})
	// TODO complete
	msg := NewMessage("ROUTING.v2.BI.ScambioFile_TSX_BI")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
	assert.Equal(t, "ROUTING.v2.BI.ScambioFile_TSX_BI", result)
}

func TestRouterMulti22(t *testing.T) {
	router := New()
	routed := false
	result := notAvailable
	var wg sync.WaitGroup
	wg.Add(1)
	router.Handle("AAA.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "AAA.>"
	})
	router.Handle("ROUTING.v2.FEEDBACK.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ROUTING.v2.FEEDBACK.>"
	})
	router.Handle("ROUTING.v2.>", 2, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ROUTING.v2.>"
	})
	router.Handle("AAA.>", 2, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ZZZ.>"
	})
	msg := NewMessage("ROUTING.v2.FEEDBACK.test>")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
	assert.Equal(t, "ROUTING.v2.FEEDBACK.>", result)
}

func TestRouterMulti3(t *testing.T) {
	router := New()
	routed := false
	result := notAvailable
	var wg sync.WaitGroup
	wg.Add(1)
	router.Handle("AAA.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "AAA.>"
	})
	router.Handle("ROUTING.v2.FEEDBACK.>", 1, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ROUTING.v2.FEEDBACK.>"
	})
	router.Handle("ROUTING.v2.>", 2, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ROUTING.v2.>"
	})
	router.Handle("AAA.>", 2, func(msg SubjectMsg, ps Params, _ interface{}) {
		defer wg.Done()
		routed = true
		result = "ZZZ.>"
	})
	msg := NewMessage("ROUTING.v2.>")
	_ = router.ServeNATS(msg)
	wg.Wait()
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
	assert.Equal(t, "ROUTING.v2.>", result)
}

func TestRouterInvalidInput(t *testing.T) {
	router := New()
	recv := catchPanic(func() {
		router.Handle("input.>", 1, nil)
	})
	if recv == nil {
		t.Fatal("registering empty method did not panic")
	}
}

func BenchmarkAllowed(b *testing.B) {
	handlerFunc := func(_ SubjectMsg, _ Params, _ interface{}) {}

	router := New()
	router.Handle("path", 1, handlerFunc)
	router.Handle("path.foo.>", 1, handlerFunc)

	b.Run("Global", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("*", 1)
		}
	})
	b.Run("Path", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("path", 1)
		}
	})
	b.Run("Path", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("path.foo.>", 1)
		}
	})
}

func TestRankList(t *testing.T) {
	r := New()
	r.rankIndexList = []int{2, 4, 1, 3}
	assert.False(t, r.initialized)
	rankList := r.getRankList()
	assert.True(t, r.initialized)
	assert.Equal(t, 1, rankList[0])
	assert.Equal(t, 2, rankList[1])
	assert.Equal(t, 3, rankList[2])
	assert.Equal(t, 4, rankList[3])
}
