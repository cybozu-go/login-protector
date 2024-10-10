# Teleport Support

## Overview


## How to use



## Development

```bash
# Install necessary tools.
$ make setup

# Setup Teleport CLI
$ make setup-teleport-cli

# Start a test Kubernetes cluster.
$ make start-kind

# Deploy Teleport
$ make deploy-teleport
```

```bash
$ make create-teleport-users
```

You can see the following output:

```console
User "api-access" has been created but requires a password. Share this URL with the user to complete user setup, link is valid for 1h:
https://localhost:3080/web/invite/xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

NOTE: Make sure localhost:3080 points at a Teleport proxy which users can access.
```

Open the URL in your browser and set the password for `myuser` and `api-access`.
You should set up MFA(Multi-Factor Authentication).

Then, you can get the token to access Teleport API.

```bash
$ ./teleport/tsh login --proxy=localhost:3080 --user=api-access --insecure --ttl=5256000
$ ./teleport/tctl --auth-server=localhost:3025 auth sign --ttl=87500h --user=api-access --out=./config/teleport/api-access.pem
```

Finally, you can deploy login-protector.

```bash
# Load container images.
make load-image

# Deploy login-protector.
$ kustomize build ./config/teleport | kubectl apply -f -
```


## Create Bot



```console

```

## Teleport Version

Teleport rejects connections from clients running incompatible versions.
login-protector should use a version of Teleport's API library that matches the major version of Teleport running in your cluster.

Run the following command from the teleport repository to find the pseudoversion:

```
go list -f '{{.Version}}' -m "github.com/gravitational/teleport/api@$(git rev-parse v15.3.7)"
v0.0.0-20240523232127-d8e06e874f3b
```

Put the pseudoversion to `go.mod` file.

https://goteleport.com/docs/admin-guides/api/getting-started/
