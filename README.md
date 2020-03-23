# NATSRouter [![Go Report Card](https://goreportcard.com/badge/github.com/mondora/natsrouter)](https://goreportcard.com/report/github.com/mondora/natsrouter)

# natsrouter
A high performance NATS client router that scales well

Trie management derived from Julien Schmidt's [httprouter](github.com/julienschmidt/httprouter), but adapted to the notation of the NATS topics

## Install
```shell script
go get github.com/mondora/natsrouter
```

## Usage example

```go
import "github.com/mondora/natsrouter"

func (cfg *Config) SubscribeListener() {
	cfg.nmux = natsrouter.New()
    // "input.:guid.v1.ping.>" OR "input.*.v1.ping.>"
    cfg.nmux.Handle("SUB", "input.*.v1.ping.>", func(msg *nats.Msg, ps natsrouter.Params, message interface{}) {
        m := message.(*Pipeline)
        m.processPing()
    })
    // "input.:guid.v1.msg.>" (OR "input.*.v1.msg.>")
    cfg.nmux.Handle("SUB", "input.*.v1.msg.>", func(msg *nats.Msg, ps natsrouter.Params, message interface{}) {
        m := message.(*Pipeline)
        m.processMessage()
    })
    ...
    _, err := cfg.natsCli.QueueSubscribe(inputSubscription, queueName, cfg.listenerHandler)
    ...
}

func (cfg *Config) listenerHandler(msg *nats.Msg) {
	message := NewListenerPipeline(cfg, msg)
	err := cfg.nmux.ServeNATSWithPayload(msg, message)
	if err != nil {
		// 404 Not Found
	}
}
```