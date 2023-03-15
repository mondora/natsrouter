# NATSRouter [![Go Report Card](https://goreportcard.com/badge/github.com/mondora/natsrouter)](https://goreportcard.com/report/github.com/mondora/natsrouter)

# natsrouter
A high performance NATS client router that scales well

Trie management derived from Julien Schmidt's [httprouter](https://github.com/julienschmidt/httprouter), but adapted to the notation of the NATS topics

## Install
```shell script
go get github.com/mondora/natsrouter/v2
```

## Usage example

Basic complete example

```go
package main

import (
	"log"
	"time"

	"github.com/mondora/natsrouter/v2"
	"github.com/nats-io/nats.go"
)

type Config struct {
	nMux *natsrouter.Router
	// ...
}

type SubMsg struct {
	msg *nats.Msg
	sub string
}

func (sm *SubMsg) GetMsg() interface{} {
	return sm.msg
}

func (sm *SubMsg) GetSubject() string {
	return sm.sub
}

func NewSubjectMsg(natsMsg *nats.Msg) natsrouter.SubjectMsg {
	subMsg := &SubMsg{
		msg: natsMsg,
		sub: natsMsg.Subject,
	}

	return subMsg
}

type Pipeline struct {
	cfg *Config
	msg *nats.Msg
}

func NewListenerPipeline(cfg *Config, msg *nats.Msg) *Pipeline {
	return &Pipeline{
		cfg: cfg,
		msg: msg,
	}
}

func (p *Pipeline) processMessage(action string) {
	log.Printf("action: %s - path: %s - data: %s\n", action, p.msg.Subject, string(p.msg.Data))
}

func (p *Pipeline) processDefault() {
	log.Printf("unmanaged path: %s\n", p.msg.Sub.Subject)
}

// ...

func (cfg *Config) SubscribeListener() {
	// "input.:guid.v1.ping.>" OR "input.*.v1.ping.>"
	cfg.nMux.Handle("input.*.v1.ping.>", 1, func(msg natsrouter.SubjectMsg, ps natsrouter.Params, message interface{}) {
		m := message.(*Pipeline)
		m.processMessage("PING")
	})
	// "input.:guid.v1.msg.>" (OR "input.*.v1.msg.>")
	cfg.nMux.Handle("input.*.v1.msg.>", 1, func(msg natsrouter.SubjectMsg, ps natsrouter.Params, message interface{}) {
		m := message.(*Pipeline)
		m.processMessage("MSG")
	})
	// Default. Rank 2 avoid collision with other valid subject ("input.*.v1.ping.>" or "input.*.v1.msg.>")
	cfg.nMux.Handle("input.*.v1.>", 2, func(msg natsrouter.SubjectMsg, ps natsrouter.Params, message interface{}) {
		m := message.(*Pipeline)
		m.processDefault()
	})
	// ...
	// queue subscribe subject must be a larger than the subjects related to the various Handlers
	// es. "input.*.v1.>" is wider than "input.*.v1.msg.>"
	// TODO enable QueueSubscribe with real NATS connection
	// _, err := natsCli.QueueSubscribe("input.*.v1.>", queueName, cfg.listenerHandler)
	// ...
}

func (cfg *Config) listenerHandler(msg *nats.Msg) {
	message := NewListenerPipeline(cfg, msg)
	// manages incoming NATS message, scanning binary tree for all defined rank
	subMsg := NewSubjectMsg(msg)
	err := cfg.nMux.ServeNATSWithPayload(subMsg, message)
	if err != nil {
		// 404 Not Found
		log.Println("404 Not Found")
	}
}

func main() {
	cfg := &Config{
		nMux: natsrouter.New(),
	}
	cfg.SubscribeListener()
	// inject msg (simulate NATS incoming message)
	cfg.listenerHandler(&nats.Msg{
		Subject: "input.TEST.v1.msg.test_action",
		Data:    []byte("TEST DATA"),
	})
	time.Sleep(1 * time.Second)
	log.Println("DONE.")
}
```