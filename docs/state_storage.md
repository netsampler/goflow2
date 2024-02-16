# State Storage

For protocols with template system (Netflow V9 and IPFIX), GoFlow2 stores the information on memory by default.
When using memory, you will lose the template and sampling rate information if GoFlow2 is restarted. So incoming
flows will fail to decode until the next template/option data is sent from the agent.

However, you can use an external state storage to overcome this issue. With external storage, you will have these
benefits:
- Supports UDP per-packet load balancer (e.g. with F5 or Envoy Proxy)
- Pod/container auto-scaling to handle traffic surge
- Persistent state, GoFlow2 restarts won't need to wait template/option data

## Memory
The default method for storing state. It's not synced across multiple GoFlow2 instances and lost on process restart.

## Redis
The supported URL format for redis
is explained at [uri specifications](https://github.com/redis/redis-specifications/blob/master/uri/redis.txt).
GoFlow2 uses the key-value storage provided by redis for persistence, and pub-sub to broadcast any new template data
to other GoFlow2 instances.
GoFlow2 also have other query parameters specific for redis:
- prefix
  - this will override key prefix and channel prefix for pubsub
  - e.g. redis://127.0.0.1/0?prefix=goflow
- interval
  - specify in seconds on how frequent we should re-retrieve values (in case the pubsub doesn't work for some reason).
    defaults to `900` seconds, use `0` to disable
  - e.g. redis://127.0.0.1/0?interval=0
