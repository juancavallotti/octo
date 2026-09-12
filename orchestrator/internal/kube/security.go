package kube

import corev1 "k8s.io/api/core/v1"

// What every container this orchestrator creates is allowed to do.
//
// Containers inherit a default Linux capability set from the container runtime,
// and CAP_NET_RAW is in it. That capability is what lets a process craft packets
// by hand and answer for an address that is not its own — which is how a pod on
// a flat network puts itself in front of another service. The orchestrator
// reaches iam over the pod network to fetch the keys it verifies every token
// with, so a deployed integration holding NET_RAW is one interception away from
// serving its own keyset and minting itself whatever roles it likes.
//
// Nothing here ever needed it. Serving HTTP is an ordinary socket: bind, listen,
// accept, none of which is a capability, and all four images this orchestrator
// runs already run as uid 65532 on a high port. Three of them are distroless and
// hold no tool that could use a raw socket anyway; the agentic runner has a shell
// but reaches programs through a `cli-run` allow list of absolute paths, which
// resolveProgram will not follow a symlink out of.
//
// Applied to the deployment pods and the dev-run pods alike: they share a
// network, so an exception in either is an exception in both.
func restricted() *corev1.SecurityContext {
	no := false
	yes := true
	uid := int64(nonrootUID)
	return &corev1.SecurityContext{
		// Capabilities are dropped wholesale rather than NET_RAW by name. The
		// others — chown, setuid, changing file ownership — are equally unused by a
		// distroless binary that runs as one non-root user, and naming one would
		// invite the list to be maintained.
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		AllowPrivilegeEscalation: &no,
		// Asserts what the images already are, so an image that stopped being
		// non-root fails to start rather than quietly running as root.
		//
		// RunAsUser is required alongside it and is not decoration. Every image
		// here declares its user by NAME (`USER nonroot`), and the kubelet cannot
		// prove a name is not root — it refuses to start the container rather than
		// guess, with "image has non-numeric user (nonroot), cannot verify user is
		// non-root". Naming the uid is what lets the check pass, and it is the same
		// uid the images already run as, so nothing about the process changes.
		RunAsNonRoot: &yes,
		RunAsUser:    &uid,
	}
}

// nonrootUID is the user every octo image runs as: the uid behind distroless's
// "nonroot", which the agentic runner's Dockerfile creates explicitly to match so
// a workspace written by one runner is readable by the other.
const nonrootUID = 65532
