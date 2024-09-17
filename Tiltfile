load('ext://restart_process', 'docker_build_with_restart')

PROTECTOR_DOCKERFILE = '''FROM golang:alpine
WORKDIR /
COPY ./bin/login-protector /
CMD ["/login-protector"]
'''

TRACKER_DOCKERFILE = '''FROM golang:alpine
WORKDIR /
COPY ./bin/local-session-tracker /
CMD ["/local-session-tracker"]
'''

# Generate manifests
local_resource('make manifests', "make manifests", deps=["api", "controllers", "hooks"], ignore=['*/*/zz_generated.deepcopy.go'])

# Don't watch generated files
watch_settings(ignore=['config/rbac/role.yaml'])

# Deploy login-protector
watch_file('./config/')
k8s_yaml(kustomize('./config/dev'))

k8s_resource(workload='login-protector-controller-manager')
local_resource('Watch & Compile', 'make build', deps=['cmd', 'internal'])

docker_build_with_restart(
    'login-protector:dev', '.',
    dockerfile_contents=PROTECTOR_DOCKERFILE,
    entrypoint=['/login-protector', '--zap-devel=true'],
    only=['./bin/login-protector'],
    live_update=[
        sync('./bin/login-protector', '/login-protector'),
    ]
)

# Sample
k8s_yaml("./test/testdata/statefulset.yaml")

docker_build_with_restart(
    'local-session-tracker:dev', '.',
    dockerfile_contents=TRACKER_DOCKERFILE,
    entrypoint=['/local-session-tracker', '--zap-devel=true'],
    only=['./bin/local-session-tracker'],
    live_update=[
        sync('./bin/local-session-tracker', '/local-session-tracker'),
    ]
)
