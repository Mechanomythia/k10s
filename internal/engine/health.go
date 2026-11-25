package engine

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// recentRestart is how long after a container terminated it still counts as
// a restart that degrades a Running pod.
const recentRestart = 10 * time.Minute

// HealthOf maps an object to the state of its star glyph. It is pure and
// never panics on missing fields; kinds without a rule are HealthNone.
func HealthOf(obj *unstructured.Unstructured, now time.Time) Health {
	group := obj.GroupVersionKind().Group
	switch {
	case group == "" && obj.GetKind() == "Pod":
		return podHealth(obj, now)
	case group == "" && obj.GetKind() == "Node":
		return nodeHealth(obj)
	case group == "apps" && (obj.GetKind() == "Deployment" || obj.GetKind() == "StatefulSet" || obj.GetKind() == "ReplicaSet"):
		return replicasHealth(obj)
	case group == "apps" && obj.GetKind() == "DaemonSet":
		return daemonSetHealth(obj)
	case group == "batch" && obj.GetKind() == "Job":
		return jobHealth(obj)
	}
	return HealthNone
}

func podHealth(obj *unstructured.Unstructured, now time.Time) Health {
	if obj.GetDeletionTimestamp() != nil {
		return HealthPending
	}
	var statuses []any
	for _, f := range []string{"initContainerStatuses", "containerStatuses"} {
		s, _, _ := unstructured.NestedSlice(obj.Object, "status", f)
		statuses = append(statuses, s...)
	}
	allReady, recent := true, false
	for _, s := range statuses {
		cs, _ := s.(map[string]any)
		switch reason, _, _ := unstructured.NestedString(cs, "state", "waiting", "reason"); reason {
		case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError":
			return HealthFailed
		}
		if ready, _, _ := unstructured.NestedBool(cs, "ready"); !ready {
			allReady = false
		}
		if at, _, _ := unstructured.NestedString(cs, "lastState", "terminated", "finishedAt"); at != "" {
			if t, err := time.Parse(time.RFC3339, at); err == nil && now.Sub(t) < recentRestart {
				recent = true
			}
		}
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	switch phase {
	case "Failed":
		return HealthFailed
	case "Succeeded":
		return HealthStopped
	case "Running":
		if recent || !allReady {
			return HealthDegraded
		}
		return HealthOK
	}
	return HealthPending
}

func replicasHealth(obj *unstructured.Unstructured) Health {
	desired, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if !found {
		desired = 1
	}
	if desired == 0 {
		return HealthStopped
	}
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); !found {
		return HealthNone
	}
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	updated, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedReplicas")
	switch {
	case updated < desired:
		return HealthPending
	case ready == desired && updated == desired:
		return HealthOK
	}
	return HealthDegraded
}

func daemonSetHealth(obj *unstructured.Unstructured) Health {
	desired, found, _ := unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
	if !found {
		return HealthNone
	}
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "numberReady")
	updated, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedNumberScheduled")
	switch {
	case desired == 0:
		return HealthStopped
	case ready == desired:
		return HealthOK
	case updated < desired:
		return HealthPending
	}
	return HealthDegraded
}

// conditionStatus returns the status of the named status condition, or "".
func conditionStatus(obj *unstructured.Unstructured, typ string) string {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if t, _, _ := unstructured.NestedString(m, "type"); t == typ {
			s, _, _ := unstructured.NestedString(m, "status")
			return s
		}
	}
	return ""
}

func jobHealth(obj *unstructured.Unstructured) Health {
	if conditionStatus(obj, "Complete") == "True" {
		return HealthStopped
	}
	if conditionStatus(obj, "Failed") == "True" {
		return HealthFailed
	}
	if active, _, _ := unstructured.NestedInt64(obj.Object, "status", "active"); active > 0 {
		return HealthPending
	}
	return HealthNone
}

func nodeHealth(obj *unstructured.Unstructured) Health {
	switch conditionStatus(obj, "Ready") {
	case "True":
		return HealthOK
	case "Unknown":
		return HealthDegraded
	case "False":
		return HealthFailed
	}
	return HealthNone
}
