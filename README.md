# NATSRouter [![Go Report Card](https://goreportcard.com/badge/github.com/mondora/natsrouter)](https://goreportcard.com/report/github.com/mondora/natsrouter)

# natsrouter
A high performance NATS client router that scales well

Trie management derived from Julien Schmidt's [httprouter](https://github.com/julienschmidt/httprouter), but adapted to the notation of the NATS topics

## Install
```shell script
go get github.com/mondora/natsrouter
```

## Usage example

Basic complete example

```go
import (
    "github.com/mondora/natsrouter"
    "fmt"
)

type Config struct {
    nmux *natsrouter.Router
    // ...
}

type Pipeline struct {
    cfg *Config
    msg *nats.Msg
}

func NewListenerPipeline(cfg *Config, msg *nats.Msg) *Pipeline {
    return &Pipeline{
        cfg:   cfg,
        msg:   msg,
    }
}

func (*p Pipeline) processPing() {
    fmt.Println("PING")
}
// ...

func (cfg *Config) SubscribeListener() {
    // "input.:guid.v1.ping.>" OR "input.*.v1.ping.>"
    cfg.nmux.Handle("input.*.v1.ping.>", 1, func(msg *nats.Msg, ps natsrouter.Params, message interface{}) {
        m := message.(*Pipeline)
        m.processPing()
    })
    // "input.:guid.v1.msg.>" (OR "input.*.v1.msg.>")
    cfg.nmux.Handle("input.*.v1.msg.>", 1, func(msg *nats.Msg, ps natsrouter.Params, message interface{}) {
        m := message.(*Pipeline)
        m.processMessage()
    })
    // Default. Rank 2 avoid collision with other valid subject ("input.*.v1.ping.>" or "input.*.v1.msg.>")
    cfg.nmux.Handle("input.*.v1.>", 2, func(msg *nats.Msg, ps natsrouter.Params, message interface{}) {
        m := message.(*Pipeline)
        m.processDefault()
    })
    ...
    // queue subscribe subject must be a larger than the subjects related to the various Handlers
    // es. "input.*.v1.>" is wider than "input.*.v1.msg.>"
    _, err := cfg.natsCli.QueueSubscribe("input.*.v1.>", queueName, cfg.listenerHandler)
    ...
}

func (cfg *Config) listenerHandler(msg *nats.Msg) {
    message := NewListenerPipeline(cfg, msg)
    // manages incoming NATS message, scanning binary tree for all defined rank
    err := cfg.nmux.ServeNATSWithPayload(msg, message)
    if err != nil {
        // 404 Not Found
    }
}

func main() {
    cfg := &Config{
        nmux: natsrouter.New()
    }
    cfg.SubscribeListener()
}
```