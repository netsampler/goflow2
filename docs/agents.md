# Agents

There are various agents that can send samples to a flow collector.

## Hardware

### Juniper

In the latest versions, Juniper supports sFlow and IPFIX protocols.

[Documentation](https://www.juniper.net/documentation/us/en/software/junos/network-mgmt/topics/topic-map/sflow-monitoring-technology.html).

Sample configuration:
```
set protocols sflow collector 10.0.0.1
set protocols sflow collector udp-port 6343
set protocols sflow interface ge-0/0/0
set protocols sflow sample-rate 2048
```

## Software

### hsflowd

[Documentation](https://sflow.net/host-sflow-linux-config.php).

Sample packets using pcap, iptables nflog and many more. Uses sFlow.

Sample configuration:
```
sflow {
  collector { ip = 10.0.0.1 udpport = 6343 }
  pcap { dev = eth0 }
}
```

Run with
```bash
$ hsflowd -d -f hsflowd.conf
```

### nProbe

[Documentation](https://www.ntop.org/guides/nprobe/)

Sample packets using pcap, iptables nflog and many more. Uses NetFlow v9 or IPFIX.

Run with
```bash
$ nprobe -i eth0 -n 10.0.0.1:2055 -V 10
```

### softflowd

[Documentation](https://github.com/irino/softflowd/blob/master/softflowd.md)

Sample packets using pcap. Uses NetFlow or IPFIX.

Run with
```bash
$ softflowd -i eth0 -n '10.0.0.1:2055' -P 'udp' -v 10
```
