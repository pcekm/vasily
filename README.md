# Graphping

Graphping is a network troubleshooting utility that's a combination of ping,
traceroute, and top. It repeatedly pings multiple hosts and displays the results
in an easy-to-read format.


## Building

To force the use of raw sockets on an OS like MacOS that would normally use
unprivileged ICMP:

```shell
go build -tags=rawsock .
```
