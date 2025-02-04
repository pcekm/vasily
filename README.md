# Vasily

[![Go](https://github.com/pcekm/vasily/actions/workflows/go.yml/badge.svg)](https://github.com/pcekm/vasily/actions/workflows/go.yml)

Vasily is a network troubleshooting utility that's a combination of ping,
traceroute, and top. It repeatedly pings multiple hosts and displays the results
in an easy-to-read format.

![](demo.gif)

## Requirements

Vasily has been tested with Go 1.23 on recent versions of:

- macOS (both unprivileged ICMP and raw sockets)
- Linux (both unprivileged ICMP and raw sockets)
- FreeBSD
- OpenBSD

It will likely also work on other UNIX systems that Go supports, provided they
have standard support for raw sockets. Windows is currently unsupported.

## Building

You will need [Go](https://go.dev/doc/install) version 1.23 or higher:

```shell
git clone https://github.com/pcekm/vasily
cd vasily
./scripts/buildrelease.sh
```

By default, Linux and macOS use unprivileged ICMP. To force the use of raw
sockets, build with the `rawsock` tag:

```shell
./scripts/buildrelease.sh -tags=rawsock
```

## Installing

For Linux and macOS systems using unprivileged ICMP, build the program and copy
it to where you want it (e.g. `/usr/local/bin`). (But see the caveats about
unprivileged ICMP on Linux below.)

```shell
cp vasily /usr/local/bin
```

For other systems that use raw sockets, you will also need to adjust its
ownership and permissions so that it runs setuid root.

```shell
cp vasily /usr/local/bin
chown 0:0 /usr/local/bin/vasily
chmod u+s /usr/local/bin/vasily
```

### Linux unprivileged ICMP

Depending on your distribution, you may need to adjust a setting on your Linux
machine to enable unprivileged pings. If it panics with
`listen error: permission denied`, change `net.ipv4.ping_group_range`. Something
like this, followed by a reboot should do it:

```shell
# As root:
printf 'net.ipv4.ping_group_range=0\t10000\n' >> /etc/sysctl.conf
```

See the [ICMP manpage](https://man7.org/linux/man-pages/man7/icmp.7.html) for
more information.

If you can't make it work, you can also build with the rawsock tag and install
it setuid root as described above.
