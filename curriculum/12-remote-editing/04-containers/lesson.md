---
title: Containers and other hops
summary: Reach files that belong to another user or live inside a container by chaining hops in a TRAMP path, edit them, run a command there, and close just that connection.
minutes: 10
---

A TRAMP path can chain hops: each `|` adds one more step from the machine before it. Lesson 12.3 used `sudo` to become root. The same shape gets you into a container on a server, or into another user's account: the method says how to get a shell there, the part before `@` says as whom.

:::vscode
Dev Containers attaches VS Code to a running container through the Docker CLI. TRAMP's container methods do the same with a path: `/docker:web:/app/` opens `/app/` inside the container *web*. Nothing is installed in the container; it only needs a shell.
:::

## Container paths

| Path | Where it opens |
|---|---|
| `/docker:web:/app/` | container *web* on this machine, through `docker exec` |
| `/podman:web:/app/` | the same with Podman |
| `/docker:root@web:/etc/` | as root inside the container |
| `/ssh:server\|docker:web:/app/` | container *web* on *server*: ssh first, then `docker` runs there |
| `/kubernetes:pod:/` | a pod, through `kubectl` |

Each method needs its command-line tool on the machine where that hop starts, and `C-x C-f /docker: TAB` lists the running containers.

The playground's box runs no container runtime, so this lesson practises the identical hop with `sudo`: the box has a *sidecar* folder that only the web server's user, *www-data*, may read. `/ssh:box|sudo:www-data@box:/srv/sidecar/` is "ssh to box, then become www-data there", the same shape as `/ssh:box|docker:www-data@web:/srv/sidecar/` would be.

## Commands follow the hop

Everything that runs a program runs it at the end of the chain: :try[M-!] (`shell-command`) in a buffer of the sidecar runs as www-data, `C-x g` would run git as www-data, `M-x compile` builds as www-data. When you are done there, `M-x tramp-cleanup-this-connection` closes that hop alone and keeps the others.

::steps
