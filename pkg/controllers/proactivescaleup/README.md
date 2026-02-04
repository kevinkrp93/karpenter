# Proactive Scale-Up Feature

This feature implements proactive scale-up similar to Kubernetes Cluster Autoscaler's proactive scale-up functionality (see [kubernetes/autoscaler#7145](https://github.com/kubernetes/autoscaler/pull/7145)).

## Overview

The proactive scale-up feature allows Karpenter to scale up capacity **before** all pods are created by workload controllers. Instead of waiting for pods to appear as pending, Karpenter monitors workload definitions (Deployments, ReplicaSets, StatefulSets, Jobs) and injects "fake pods" into its internal provisioning calculations.

### Key Benefits

- **Faster scale-up**: Nodes are provisioned immediately when workloads are scaled, not after pods become pending
- **Zero Kubernetes overhead**: Fake pods exist only in Karpenter's memory, never as actual Kubernetes objects
- **Transparent**: Fake pods are not visible via `kubectl get pods`
- **Backward compatible**: Feature is disabled by default

## How It Works

1. **Workload Monitoring**: The injector watches Deployments, ReplicaSets, StatefulSets, and Jobs
2. **Gap Calculation**: For each workload, it calculates: `desired replicas - actual pods`
3. **Fake Pod Injection**: Creates in-memory pod objects matching the workload's pod template
4. **Provisioning**: Karpenter provisions nodes based on real + fake pods
5. **Real Pod Scheduling**: When real pods are created, they schedule immediately on the pre-provisioned nodes
6. **Next Cycle**: Gap becomes 0, no fake pods injected

## Configuration

### Enable the Feature

The feature is controlled by a feature gate and is **disabled by default**:

```bash
# Using feature gates flag
--feature-gates=ProactiveScaleUp=true

# Using environment variable
FEATURE_GATES="ProactiveScaleUp=true"
```

### Configuration Options

#### Pod Injection Limit

Limits the total number of fake pods that can be injected (default: 5000):

```bash
--pod-injection-limit=5000
# or
POD_INJECTION_LIMIT=5000
```

## Example

```yaml
# Before scale-up
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 5  # Currently 5 running pods
  # ... rest of spec

# Scale up deployment
kubectl scale deployment my-app --replicas=100

# What happens:
# 1. Deployment controller updates desired replicas to 100
# 2. Karpenter injector detects gap: 100 - 5 = 95 pods
# 3. Injector creates 95 fake pods in memory
# 4. Karpenter provisions nodes for 100 pods (5 real + 95 fake)
# 5. Deployment controller creates real pods
# 6. Real pods schedule instantly on pre-provisioned nodes
# 7. Next cycle: gap = 0, no fake pods
```

## Fake Pod Details

Fake pods are created with:
- All resource requests and limits from the pod template
- Node selectors, affinity, tolerations
- Topology spread constraints
- Label: `karpenter.sh/fake-pod=true`
- Status: Pending/Unschedulable

Fake pods DO NOT have:
- Volume claims
- Init containers (resources copied but not executed)
- Service account tokens
- Owner references in Kubernetes API

## Supported Workloads

- ✅ Deployments
- ✅ ReplicaSets (standalone, not managed by Deployments)
- ✅ StatefulSets
- ✅ Jobs (based on `parallelism`)

## Limitations

- Fake pods are filtered out of pod counts to prevent double-counting on subsequent cycles
- Maximum injection limit prevents resource exhaustion
- Only considers workloads that Karpenter can schedule (respects node selectors, affinity, etc.)

## Backward Compatibility

- Feature is **disabled by default** (`ProactiveScaleUp=false`)
- No API changes required
- No impact when disabled
- Can be enabled/disabled at runtime (requires restart)

## Implementation Reference

Based on:
- Azure Karpenter PR: https://github.com/Azure/karpenter-provider-azure/pull/1399
- Cluster Autoscaler PR: https://github.com/kubernetes/autoscaler/pull/7145
