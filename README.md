[![GitHub release](https://img.shields.io/github/release/cybozu-go/login-protector.svg?maxAge=60)][releases]
[![CI](https://github.com/cybozu-go/login-protector/actions/workflows/ci.yaml/badge.svg)](https://github.com/cybozu-go/login-protector/actions/workflows/ci.yaml)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/cybozu-go/login-protector?tab=overview)](https://pkg.go.dev/github.com/cybozu-go/login-protector?tab=overview)
[![Go Report Card](https://goreportcard.com/badge/github.com/cybozu-go/login-protector)](https://goreportcard.com/report/github.com/cybozu-go/login-protector)

# login-protector

**Project Status**: Initial development

## Description

login-protector provides a feature to prevent the reboot of Pods that are logged in within a Kubernetes cluster.

## Background

In Kubernetes clusters, Pods are sometimes used as bastion servers for operations.
When these Pods are rebooted, the logged-in session is disconnected, causing operational interruptions.
login-protector prevents this by ensuring that logged-in Pods are not rebooted.

## How It Works

login-protector checks if the processes in the target Pod are using TTY to determine if the Pod is logged in.
If a Pod is found to be logged in, login-protector generates a PodDisruptionBudget with `maxUnavailable: 0` to prevent the Pod from being evicted.
This ensures that the Pod is not rebooted during maintenance or upgrades when a Kubernetes Node is drained.

Additionally, login-protector can prevent the container image or PodTemplate of logged-in Pods from being updated.
To achieve this, the Pods targeted by login-protector must be created by a StatefulSet with the updateStrategy set to OnDelete.

To avoid issues with long-term Pod logins blocking necessary Node reboots or Pod updates, it is advisable to set up alert checks for prolonged logins.
Furthermore, the PodDisruptionBudget can be disabled by adding an annotation to force a Pod reboot.

## Installation:

Run the following command to install login-protector:

```sh
kubectl apply -f https://github.com/cybozu-go/login-protector/releases/download/v0.1.0/login-protector.yaml
```

## Usage:

login-protector targets only StatefulSets. The StatefulSet should be configured as follows:

1. Add the label `login-protector.cybozu.io/protect: "true"` to the StatefulSet.
2. Add the sidecar container `ghcr.io/cybozu-go/tty-exporter` and specify `shareProcessNamespace: true`.
3. Set the `updateStrategy` to `type: OnDelete`.


Example manifest:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: target-sts
  labels:
    login-protector.cybozu.io/protect: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      name: target-sts
  serviceName: target-sts
  template:
    metadata:
      labels:
        name: target-sts
    spec:
      containers:
      - name: main
        image: ghcr.io/cybozu/ubuntu:22.04
        imagePullPolicy: IfNotPresent
        command: [ "sleep", "infinity" ]
      - name: tty-exporter
        image: ghcr.io/cybozu-go/tty-exporter:latest
        imagePullPolicy: IfNotPresent
        ports:
        - name: sidecar
          containerPort: 8080
      shareProcessNamespace: true
  updateStrategy:
    type: OnDelete
```

## Annotations

Annotations can be used to modify the behavior of login-protector for the target StatefulSet:

- `login-protector.cybozu.io/exporter-name`: Specify the name of the tty-exporter sidecar container. Default is "tty-exporter".
- `login-protector.cybozu.io/exporter-port`: Specify the port of the tty-exporter sidecar container. Default is "8080".
- `login-protector.cybozu.io/no-pdb`: Set to "true" to prevent the creation of a PodDisruptionBudget.


```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: target-sts
  labels:
    login-protector.cybozu.io/protect: "true"
  annotations:
    login-protector.cybozu.io/exporter-name: sidecar
    login-protector.cybozu.io/exporter-port: "9090"
    login-protector.cybozu.io/no-pdb: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      name: target-sts
  serviceName: target-sts
  template:
    metadata:
      labels:
        name: target-sts
    spec:
      containers:
        - name: main
          image: ghcr.io/cybozu/ubuntu:22.04
          imagePullPolicy: IfNotPresent
          command: [ "sleep", "infinity" ]
        - name: sidecar
          image: ghcr.io/cybozu-go/tty-exporter:latest
          imagePullPolicy: IfNotPresent
          ports:
            - name: sidecar
              containerPort: 9090
      shareProcessNamespace: true
  updateStrategy:
    type: OnDelete
```

## Development

Install Golang, Docker, Make, and aqua beforehand.

### With Tilt

[Tilt](https://tilt.dev/) is a local development tool that makes it easy to develop applications for Kubernetes.
You can use Tilt to automatically build and deploy login-protector.

```bash
# Install necessary tools.
make setup

# Start a test Kubernetes cluster.
make start-dev

# Start development with Tilt.
tilt up
```

Access the Tilt dashboard at http://localhost:10350.
Changes to the source code or manifests will be automatically reflected.

### Without Tilt

If you don't want to use Tilt, you can use the following commands:

```bash
# Install necessary tools.
make setup

# Start a test Kubernetes cluster.
make start-kind

# Load container images.
make load-image

# Deploy login-protector.
make deploy

# Deploy test StatefulSets.
kubectl apply -f ./test/testdata/statefulset.yaml
```

## Release Process

T.B.D.

## License

Apache License 2.0

[releases]: https://github.com/cybozu-go/login-protector/releases
