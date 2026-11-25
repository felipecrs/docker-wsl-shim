# docker-wsl-shim

A `docker.exe` for Windows that runs the docker CLI of your default WSL distribution. It gives Windows programs a `docker` command without Docker Desktop.

## Why?

Docker runs fine in WSL 2 without Docker Desktop, but then only WSL has a `docker` command. Scripts, editors and tools on Windows that call `docker` don't find it.

Calling `wsl docker` instead doesn't quite work: Windows paths mean nothing to it, environment variables are lost, and most programs can't be told to call something else.

## Highlights

- **Windows paths work**: in `--volume`, `--mount`, `--cidfile`, `cp`, compose files and more. `C:\a` becomes `/mnt/c/a`, `\\wsl.localhost\Ubuntu\a` becomes `/a`, and `.\a` becomes `./a`.
- **Builds work**: `docker build`, `buildx build`, `buildx bake` and `compose build`, with contexts, secrets, outputs, caches and metadata files on Windows paths.
- **What runs inside the container is left alone.** The options of docker 18.03 to 29 are built in, and newer ones are learned from `docker --help`.
- **Fast**: a single call to `wsl.exe`, about 20 ms slower than calling `wsl docker` yourself.
- **Arguments reach docker unchanged.** There is no shell in between.
- **Environment variables are passed on**, so that `-e NAME`, `--secret env=NAME` and `${NAME}` in compose files work. `PATH`, `HOME` and others that mean something else on Linux are not.
- **Terminals, Ctrl+C, streams, exit codes and the working directory** behave as they do with the real CLI. If `docker.exe` is killed, the client in WSL ends too.

## Installation

With [mise](https://mise.jdx.dev):

```powershell
mise use -g github:felipecrs/docker-wsl-shim
```

Or put `docker.exe` from the [latest release](https://github.com/felipecrs/docker-wsl-shim/releases/latest) in your `PATH`.

It requires Docker in the default WSL 2 distribution, usable without `sudo`.

## Try it yourself

```powershell
docker run --rm -v C:\Windows\System32\drivers\etc:/etc/windows:ro alpine cat /etc/windows/hosts
```

See what it would run, without running it:

```powershell
$env:DOCKER_WSL_SHIM_PRINT_COMMAND = "true"
docker run --rm -v C:\a:/a alpine ls C:\a
# docker run --rm -v /mnt/c/a:/a alpine ls C:\a
```

## Configuration

| Environment variable | Description |
|---|---|
| `DOCKER_WSL_SHIM_DOCKER_PATH` | The docker to run in WSL. Default: `docker` |
| `DOCKER_WSL_SHIM_PRINT_COMMAND` | `true` prints the command instead of running it |

## Limits

- Only programs that call the `docker` command are helped. Programs that talk to the Docker API themselves need an endpoint (`\\.\pipe\docker_engine` or `DOCKER_HOST`), which this does not provide.
- Windows paths written inside compose and bake files are not translated. Relative ones work.
- Ctrl+Break ends the client instead of reaching the container.
- Drives are expected under `/mnt`, and `\\wsl.localhost\<distro>` is taken for the default distribution.

## Development

```powershell
go test ./...
```

The end-to-end tests use the `alpine` image and are skipped when WSL has no Docker.

## See also

- [docker-on-docker-shim](https://github.com/felipecrs/docker-on-docker-shim), which makes volume mounts work when running docker on docker.
