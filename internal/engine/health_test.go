package engine

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type m = map[string]any

func obj(apiVersion, kind string, spec, status m) *unstructured.Unstructured {
	o := m{"apiVersion": apiVersion, "kind": kind, "metadata": m{"name": "x"}}
	if spec != nil {
		o["spec"] = spec
	}
	if status != nil {
		o["status"] = status
	}
	return &unstructured.Unstructured{Object: o}
}

func pod(status m) *unstructured.Unstructured { return obj("v1", "Pod", nil, status) }

func cs(ready bool, state, last m) m {
	c := m{"ready": ready}
	if state != nil {
		c["state"] = state
	}
	if last != nil {
		c["lastState"] = last
	}
	return c
}

func running(ready bool, last m) m {
	return m{"phase": "Running", "containerStatuses": []any{cs(ready, nil, last)}}
}

func waiting(reason string) m { return m{"waiting": m{"reason": reason}} }

func cond(typ, status string) m {
	return m{"conditions": []any{m{"type": typ, "status": status}}}
}

func TestHealthOf(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) m {
		return m{"terminated": m{"finishedAt": now.Add(-d).Format(time.RFC3339)}}
	}
	deleting := pod(m{"phase": "Running"})
	del := metav1.NewTime(now)
	deleting.SetDeletionTimestamp(&del)
	deploy := func(replicas any, ready, updated int64) *unstructured.Unstructured {
		var spec m
		if replicas != nil {
			spec = m{"replicas": replicas}
		}
		return obj("apps/v1", "Deployment", spec, m{"readyReplicas": ready, "updatedReplicas": updated})
	}
	ds := func(desired, ready, updated int64) *unstructured.Unstructured {
		return obj("apps/v1", "DaemonSet", nil, m{"desiredNumberScheduled": desired, "numberReady": ready, "updatedNumberScheduled": updated})
	}

	tests := []struct {
		name string
		o    *unstructured.Unstructured
		want Health
	}{
		{"pod no status", obj("v1", "Pod", nil, nil), HealthPending},
		{"pod pending", pod(m{"phase": "Pending"}), HealthPending},
		{"pod deleting", deleting, HealthPending},
		{"pod failed phase", pod(m{"phase": "Failed"}), HealthFailed},
		{"pod succeeded", pod(m{"phase": "Succeeded"}), HealthStopped},
		{"pod crashloop", pod(m{"phase": "Running", "containerStatuses": []any{cs(false, waiting("CrashLoopBackOff"), nil)}}), HealthFailed},
		{"pod pending imagepull", pod(m{"phase": "Pending", "containerStatuses": []any{cs(false, waiting("ImagePullBackOff"), nil)}}), HealthFailed},
		{"pod errimagepull", pod(m{"phase": "Pending", "containerStatuses": []any{cs(false, waiting("ErrImagePull"), nil)}}), HealthFailed},
		{"pod config error", pod(m{"phase": "Pending", "containerStatuses": []any{cs(false, waiting("CreateContainerConfigError"), nil)}}), HealthFailed},
		{"pod init crashloop", pod(m{"phase": "Pending", "initContainerStatuses": []any{cs(false, waiting("CrashLoopBackOff"), nil)}}), HealthFailed},
		{"pod containercreating stays pending", pod(m{"phase": "Pending", "containerStatuses": []any{cs(false, waiting("ContainerCreating"), nil)}}), HealthPending},
		{"pod running ready", pod(running(true, nil)), HealthOK},
		{"pod running no statuses", pod(m{"phase": "Running"}), HealthOK},
		{"pod running not ready", pod(running(false, nil)), HealthDegraded},
		{"pod restart 9m ago", pod(running(true, ago(9*time.Minute))), HealthDegraded},
		{"pod restart 11m ago", pod(running(true, ago(11*time.Minute))), HealthOK},

		{"deploy ok", deploy(int64(3), 3, 3), HealthOK},
		{"deploy default replicas", deploy(nil, 1, 1), HealthOK},
		{"deploy zero", deploy(int64(0), 0, 0), HealthStopped},
		{"deploy zero no status", obj("apps/v1", "Deployment", m{"replicas": int64(0)}, nil), HealthStopped},
		{"deploy no status", obj("apps/v1", "Deployment", m{"replicas": int64(2)}, nil), HealthNone},
		{"deploy rolling", deploy(int64(3), 3, 1), HealthPending},
		{"deploy unready", deploy(int64(3), 2, 3), HealthDegraded},
		{"statefulset ok", obj("apps/v1", "StatefulSet", m{"replicas": int64(2)}, m{"readyReplicas": int64(2), "updatedReplicas": int64(2)}), HealthOK},
		{"replicaset degraded", obj("apps/v1", "ReplicaSet", m{"replicas": int64(2)}, m{"readyReplicas": int64(1), "updatedReplicas": int64(2)}), HealthDegraded},
		{"apps wrong group", obj("example.com/v1", "Deployment", m{"replicas": int64(0)}, nil), HealthNone},
		{"ds ok", ds(3, 3, 3), HealthOK},
		{"ds zero", ds(0, 0, 0), HealthStopped},
		{"ds updating", ds(3, 2, 1), HealthPending},
		{"ds degraded", ds(3, 2, 3), HealthDegraded},
		{"ds no status", obj("apps/v1", "DaemonSet", nil, nil), HealthNone},

		{"job complete", obj("batch/v1", "Job", nil, cond("Complete", "True")), HealthStopped},
		{"job failed", obj("batch/v1", "Job", nil, cond("Failed", "True")), HealthFailed},
		{"job active", obj("batch/v1", "Job", nil, m{"active": int64(1)}), HealthPending},
		{"job complete false", obj("batch/v1", "Job", nil, cond("Complete", "False")), HealthNone},
		{"job no status", obj("batch/v1", "Job", nil, nil), HealthNone},

		{"node ready", obj("v1", "Node", nil, cond("Ready", "True")), HealthOK},
		{"node unknown", obj("v1", "Node", nil, cond("Ready", "Unknown")), HealthDegraded},
		{"node not ready", obj("v1", "Node", nil, cond("Ready", "False")), HealthFailed},
		{"node no status", obj("v1", "Node", nil, nil), HealthNone},

		{"other kind", obj("v1", "ConfigMap", nil, nil), HealthNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HealthOf(tc.o, now); got != tc.want {
				t.Errorf("HealthOf = %q, want %q", got, tc.want)
			}
		})
	}
}
