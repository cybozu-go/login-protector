package common

const LabelKeyLoginProtectorProtect = "login-protector.cybozu.io/protect"
const AnnotationKeyNoPDB = "login-protector.cybozu.io/no-pdb"
const AnnotationKeyTrackerName = "login-protector.cybozu.io/tracker-name"
const AnnotationKeyTrackerPort = "login-protector.cybozu.io/tracker-port"
const AnnotationLoggedIn = "login-protector.cybozu.io/logged-in"

const DefaultTrackerName = "local-session-tracker"
const DefaultTrackerPort = "8080"
const ValueTrue = "true"
const ValueFalse = "false"
const KindStatefulSet = "StatefulSet"
const KindPod = "Pod"
