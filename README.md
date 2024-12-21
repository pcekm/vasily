# Graphping

[![Go](https://github.com/pcekm/graphping/actions/workflows/go.yml/badge.svg)](https://github.com/pcekm/graphping/actions/workflows/go.yml)

Graphping is a network troubleshooting utility that's a combination of ping,
traceroute, and top. It repeatedly pings multiple hosts and displays the results
in an easy-to-read format.

![](demo.gif)

## Requirements

Graphping has been tested on recent versions of:

- macOS (both unprivileged ICMP and raw sockets)
- Linux (both unprivileged ICMP and raw sockets)
- FreeBSD
- OpenBSD

It will likely also work on other UNIX systems that Go supports, provided they
have standard support for raw sockets. Windows is currently unsupported.

## Building

You will need [Go](https://go.dev/doc/install) version 1.23 or higher:

```shell
git clone https://github.com/pcekm/graphping
cd graphping
go build .
```

By default, Linux and macOS use unprivileged ICMP. To force the use of raw
sockets, build with the `rawsock` tag:

```shell
go build -tags=rawsock .
```

## Installing

For Linux and macOS systems using unprivileged ICMP, build the program and copy
it to where you want it (e.g. `/usr/local/bin`). (But see the caveats about
unprivileged ICMP on Linux below.)

```shell
cp graphping /usr/local/bin
```

For other systems that use raw sockets, you will also need to adjust its
ownership and permissions so that it runs setuid root.

```shell
cp graphping /usr/local/bin
chown 0:0 /usr/local/bin/graphping
chmod u+s /usr/local/bin/graphping
```

### Linux unprivileged ICMP

Depending on your distribution, you may need to adjust a setting on your Linux
machine to enable unprivileged pings. If it panics with
`listen error: permission denied`, you'll need to change the
`net.ipv4.ping_group_range` setting. Something like this, followed by a reboot
should do it:

```shell
# As root:
printf 'net.ipv4.ping_group_range=0\t10000\n' >> /etc/sysctl.conf
```

See the [ICMP manpage](https://man7.org/linux/man-pages/man7/icmp.7.html) for
more information.

If you can't make it work, you can also build with the rawsock tag and install
it setuid root as described above.
