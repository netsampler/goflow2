# Performance

When setting up GoFlow2 for the first time, it is difficult to estimate the settings and resources required.
This software has been tested with hundreds of thousands of flows per second on common hardware but the default settings may not be optimal everywhere.

It is important to understand the pattern of your flows.
Some environments have predictable trends, for instance a regional ISP will likely have a peak of traffic at 20:00 local time,
whereas a hosting provider may have large bursts of traffic due to a DDoS attack.

We need to consider the following:

* R: The rate of packets (controlled by sampling and traffic)
* C: The decoding capacity of a worker (dependent on CPU)
* L: The allowed latency (dependent on buffer size)

In a typical environment, capacity matches or exceeds the rate (C >= R).
When the rate goes above the capacity (eg: bursts), packets waiting to be processed pile up.
Latency increases as long as the rate exceeds the capacity. It remains stable if the rate equals the capacity.
It can only lower when there is extra capacity (C-R).

A buffer too large can cause "buffer bloat" where latency is too high for normal operations (eg: DDoS detection being delayed),
whereas a short buffer (or no buffer for real-time) may drop information during an temporary increase.

The listen URI can be customized to meet an environment requirements.
GoFlow2 will work better in an environment with guaranteed resources.

## Life of a packet

When a packet is received by the collectors' machine, the kernel will send the packet towards a socket.
The socket is buffered. On Linux, the buffersize is a global configuration setting: `rmem_max`.

If the buffer is full, new packets will be discarded and increasing the count of
UDP errors.

A first level of load-balancing can be done by having multiple sockets listening
on the same port.
On Linux, this is done with `SO_REUSEPORT` and `SO_REUSEADDRESS` options.
In GoFlow2 you can set the `count` option to define the number of sockets.
Each socket will put the packet in a queue to be decoded.

The number of `workers` should ideally match the number of CPUs available.
By default, the number is set to twice the amount of open sockets.

`Blocking` mode forces GoFlow2 to operate in real-time instead of buffered. A packet is only decoded if
a worker is available and storage depends on the kernel UDP buffer.

In buffered mode, the size of the queue is set by `queue_size`, much larger than the UDP buffer.

The URI below summarizes the options:

```
$ goflow2 -listen flow://:6343/?count=4&workers=16&blocking=false&queue_size=1000000
                                ^        ^          ^              ^
                                ┃        ┃          ┃              ┗ In buffered mode, the amount of packets stored in memory
                                ┃        ┃          ┗ Real-time mode
                                ┃        ┗ Decoding workers
                                ┗ Open sockets listening
```

## Note on resources guarantees

GoFlow2 works better on guaranteed fixed resources.
It requires the operator to scope for a worst case scenario in terms of latency.

RAM usage is dependent on the `queue_size` (unless using blocking mode).
By default, this may exceed the host memory if rate is above capacity and result in an `OoM` crash.
As UDP packets can be a maximum of 9000 bytes, as a result, a 2GB RAM machine can only buffer 222222 packets if there no overhead.

Kubernetes is an example of allowing flexible resources for processes.

In a Pod `resources`, the `request` and `limits` can be set for CPU. Extra CPU can be used by other applications if colocated.
Make sure they are the same for RAM since if GoFlow2 is killed, data could be lost, unless you are confident other applications
will not require extra RAM during peaks.

Furthermore, `HorizontalPodScalers` can be used to create additional GoFlow2 instances and route the packets when a metric crosses a threshold.
This is not recommended with NetFlow/IPFIX without having a shared template system due to cold-starts.

Another item to take into account, make sure the MTU of the machines where the collector are hosted match
the MTU of the sampling routers. This can cause some issues when tunnelling or on certain cloud providers. 
