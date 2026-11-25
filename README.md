# docker-wsl-shim

A `docker.exe` for Windows that runs the docker CLI of your default WSL distribution. It gives Windows programs a `docker` command without Docker Desktop.

```powershell
docker run --rm -v C:\Users\me\project:/src alpine ls /src
docker compose -f .\compose.yml up -d
```

## Install

With [mise](https://mise.jdx.dev):

```powershell
mise use -g go go:github.com/felipecrs/docker-wsl-shim/cmd/docker
```

It requires Docker in the default WSL 2 distribution, usable without `sudo`.

## What it does

- **Arguments** reach docker unchanged. There is no shell in between.
- **Paths** are translated in docker's own arguments: `C:\a` becomes `/mnt/c/a`, `\\wsl.localhost\Ubuntu\a` becomes `/a`, and `.\a` becomes `./a`. This covers `-v`, `--mount`, `--cidfile`, `--env-file`, build contexts, `cp`, compose files and more. The command that runs inside the container is left alone.
- **Environment variables** are passed on, so that `-e NAME` and `${NAME}` in compose files work. `PATH`, `HOME` and others that mean something else on Linux are not.
- **Streams, exit codes and the working directory** behave as they do with the real CLI. If `docker.exe` is killed, the client in WSL ends too.

## Limits

- Only programs that run the `docker` command are helped. Programs that talk to the Docker API themselves need an endpoint (`\\.\pipe\docker_engine` or `DOCKER_HOST`), which this does not provide.
- Windows paths written inside compose files are not translated. Relative ones work.
- Drives are expected under `/mnt`, and `\\wsl.localhost\<distro>` is taken for the default distribution.

## Development

```powershell
go test ./...
```

The end-to-end tests use the `alpine` image and are skipped when WSL has no Docker.
