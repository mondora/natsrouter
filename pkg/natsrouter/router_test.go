package natsrouter

import (
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

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

	routed := false
	router.Handle("SUB", "user.:name", func(msg *nats.Msg, ps Params, _ interface{}) {
		routed = true
		want := Params{Param{"name", "gopher"}}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})

	msg := &nats.Msg{
		Subject: "user.gopher",
	}
	_ = router.ServeNATS(msg)
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterCatchAll(t *testing.T) {
	router := New()

	routed := false
	router.Handle("SUB", "user.*.>", func(msg *nats.Msg, ps Params, _ interface{}) {
		routed = true
		//want := Params{Param{">", ".gopher.ok"}}
		want := Params{
			Param{"p1", "gopher"},
			Param{">", ".star.ok"},
		}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})

	msg := &nats.Msg{
		Subject: "user.gopher.star.ok",
	}
	_ = router.ServeNATS(msg)
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterMulti(t *testing.T) {
	router := New()
	routed := false
	router.Handle("SUB", "confirm-subsbscription.:mongoid.:correlationid.:pincode.>", func(msg *nats.Msg, ps Params, _ interface{}) {
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
	router.Handle("SUB", "reset-subsbscription.:mongoid.>", func(msg *nats.Msg, ps Params, _ interface{}) {
		routed = true
		want := Params{
			Param{"mongoid", "1234"},
		}
		if !reflect.DeepEqual(ps, want) {
			t.Fatalf("wrong wildcard values: want %v, got %v", want, ps)
		}
	})
	msg := &nats.Msg{
		Subject: "confirm-subsbscription.1234.5678.1111.>",
	}
	_ = router.ServeNATS(msg)
	assert.True(t, routed)
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterInvalidInput(t *testing.T) {
	router := New()
	recv := catchPanic(func() {
		router.Handle("SUB", "input.>", nil)
	})
	if recv == nil {
		t.Fatal("registering empty method did not panic")
	}
}

func BenchmarkAllowed(b *testing.B) {
	handlerFunc := func(_ *nats.Msg, _ Params, _ interface{}) {}

	router := New()
	router.Handle("SUB", "path", handlerFunc)
	router.Handle("SUB", "path.foo.>", handlerFunc)

	b.Run("Global", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("*", "SUB")
		}
	})
	b.Run("Path", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("path", "SUB")
		}
	})
	b.Run("Path", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = router.allowed("path.foo.>", "SUB")
		}
	})
}
