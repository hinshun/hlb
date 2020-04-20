<h1 align="center">High level build</h1>

<div align="center">
A language to build and test any software efficiently.

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=blue)](https://pkg.go.dev/github.com/openllb/hlb)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Test](https://github.com/openllb/hlb/workflows/Test/badge.svg)](https://github.com/openllb/hlb/actions?query=workflow%3ATest)

ttygif
</div>

## Key features

- :crystal_ball: Rich error messages
- :detective: Debug builds step by step 
- :bookmark_tabs: [Language server](https://github.com/openllb/hlb-langserver) for IDE features
- :gift: Share builds as libraries
- :whale: Build existing Dockerfiles, Compose files, Buildpacks, and more

## Getting started

Head on over to [Releases](https://github.com/openllb/hlb/releases) to grab a static binary.

Or you can compile HLB yourself using [go 1.13+](https://golang.org/dl/):
```sh
GO111MODULE=on go get -u github.com/openllb/hlb/cmd/hlb
```

You'll also need to run the backend `buildkitd` somewhere you can connect to. The easiest way if you have [Docker](https://www.docker.com/get-started), is to run a BuildKit container locally:
```sh
docker run -d --name buildkitd --privileged moby/buildkit:v0.7.0
```

If you don't have Docker or you don't want Docker, you can run it on locally if you have Linux, or connect to a remote Linux machine. More instructions on [building BuildKit here](https://github.com/moby/buildkit#quick-start).

Once you have BuildKit, you can run one of the examples in `./examples`:
```sh
export BUILDKIT_HOST=docker-container://buildkitd
hlb run ./examples/node.hlb
```

## HLB 101

> For documentation, check out: https://openllb.github.io/hlb/

In HLB, builds are described with units of containerized work. However, this does not mean HLB is only for creating container images as you'll see in a moment. By containerizing your build it becomes reproducible on other machines, and composable in a more complex workflow.

We can begin by defining a function:

```hlb
fs echo() {
	image "alpine"
	run "echo hello world > /opt/foo"
}
```

The function definition begins with its `return type`, in this case a filesystem, followed by the name of the function and its arguments. The body of the function follows with commands that look very much like Dockerfiles. When we run the function `echo`, we are using a image `alpine` that writes `hello world` into a `/opt/foo` file.

Let's try running this example, write the example above into a file `example.hlb` and on your terminal run `hlb run --target echo example.hlb`:

```sh
$ hlb run --target echo example.hlb
[+] Building 0.5s (3/3) FINISHED
 => compiling [default]                                                     0.0s
 => docker-image://docker.io/library/alpine:latest                          0.4s
 => => resolve docker.io/library/alpine:latest                              0.4s
 => /bin/sh -c 'echo hello world > /opt/foo'                                0.0s
```

We can only transfer filesystems to/from BuildKit, so in order to download `/opt/foo`, we need to move the file into an empty filesystem to avoid also downloading the rest of `alpine`.

In Dockerfiles, we will typically write two stages, one for creating the artifact and one for copying the artifact into a scratch image:

```Dockerfile
FROM alpine AS build
RUN echo hello world > /opt/foo

FROM scratch
COPY --from=build /opt/foo /foo
```

Instead of copying the file, we can do better by utilizing mounts. A mount is simply a directory that is accessible at a target location known as the `mount point`. The filesystem at `/` that make up `alpine` is also a mount, and you can mount additional filesystems on top of other mounts.

```hlb
fs echo() {
	image "alpine"
	run "echo hello world > /opt/foo" with option {
		mount scratch "/opt"
	}
}
```

We extended the `run` command with an option block, which gives us the ability to configure this particular `run` command. In this example, we mounted a `scratch` filesystem at the mount point `/opt`, so that when writing to `/opt/foo`, what we're really doing is writing to `/foo` on the filesystem mounted at `/opt`.

Right now `echo` is the only available target in the example, but we want to operate on the mount's contents instead. So let's name the mount:

```hlb
fs echo() {
	image "alpine"
	run "echo hello world > /opt/foo" with option {
		mount scratch "/opt" as echoOutput
	}
}
```

Now we can execute the target and download the contents. On your terminal run `hlb run --target echoOutput,download=. example.hlb`:

```sh
$ hlb run --target echoOutput,download=. example.hlb
[+] Building 1.4s (4/4) FINISHED
 => compiling [echoOutput]                                                  0.0s
 => CACHED docker-image://docker.io/library/alpine:latest                   0.0s
 => => resolve docker.io/library/alpine:latest                              0.8s
 => /bin/sh -c 'echo hello world > /opt/foo'                                0.4s
 => exporting to client                                                     0.0s
 => => copying files 38B                                                    0.0s

$ cat foo
hello world
```
