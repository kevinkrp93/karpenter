# Proactive Scale-Up Implementation Summary

This document summarizes the implementation of proactive scale-up functionality in Karpenter, based on Azure Karpenter PR #1399 and Kubernetes Cluster Autoscaler PR #7145.

## What Was Implemented

### 1. Core Infrastructure (`pkg/controllers/provisioning/`)
- **`podinjector.go`**: New `PodInjector` interface for injecting synthetic pods
- **`provisioner.go`**: Modified to support pod injection
  - Added `podInjector` field to `Provisioner` struct
  - Added `SetPodInjector()` method to configure injector
  - Modified `GetPendingPods()` to call injector when configured

### 2. Proactive Scale-Up Controller (`pkg/controllers/proactivescaleup/`)
- **`injector.go`**: Implementation of the `PodInjector` interface
  - Monitors workload definitions (Deployments, ReplicaSets, StatefulSets, Jobs)
  - Calculates gap: `desired replicas - actual pods`
  - Generates fake pods matching workload pod templates
  - Adds label `karpenter.sh/fake-pod=true` to identify fake pods
  - Respects pod injection limits to prevent resource exhaustion
  
- **`injector_test.go`**: Unit tests for the injector

- **`README.md`**: Comprehensive documentation

### 3. Configuration & Options (`pkg/operator/options/`)
- **`options.go`**: Added configuration options
  - New feature gate: `ProactiveScaleUp` (default: **false**)
  - New option: `--pod-injection-limit` (default: 5000)
  - Updated feature gate parsing

### 4. Integration (`pkg/controllers/`)
- **`controllers.go`**: Wired up the injector
  - Creates injector when `ProactiveScaleUp` feature gate is enabled
  - Configures provisioner with the injector

## Key Design Decisions

### Backward Compatibility
- Feature defaults to **false** - no breaking changes
- No API changes required
- Can be enabled/disabled at runtime (requires controller restart)

### Implementation Approach
Following Cluster Autoscaler PR #7145:
- Fake pods exist only in memory (never created in Kubernetes)
- Fake pods are filtered from subsequent counts to avoid double-counting
- Support for common workload types: Deployments, ReplicaSets, StatefulSets, Jobs

### Proper Pod Counting for Deployments
- Deployments don't directly own pods - ReplicaSets do
- Implementation correctly counts pods owned by ReplicaSets created by Deployments
- Prevents duplicate counting when processing both Deployments and ReplicaSets

## How to Use

### Enable the Feature

```bash
# Using feature gates flag
--feature-gates=ProactiveScaleUp=true

# Using environment variable
export FEATURE_GATES="ProactiveScaleUp=true"
```

### Configure Pod Injection Limit

```bash
# Limit total number of fake pods
--pod-injection-limit=5000

# Or via environment variable
export POD_INJECTION_LIMIT=5000
```

## Example Workflow

1. User scales a Deployment from 5 to 100 replicas
2. Karpenter injector detects the gap (95 pods)
3. Injector creates 95 fake pods in memory
4. Karpenter provisions nodes for 100 pods total
5. Real pods are created by Deployment controller
6. Real pods schedule immediately on pre-provisioned nodes
7. Next cycle: gap = 0, no fake pods needed

## Files Modified/Created

### New Files
- `pkg/controllers/provisioning/podinjector.go`
- `pkg/controllers/proactivescaleup/injector.go`
- `pkg/controllers/proactivescaleup/injector_test.go`
- `pkg/controllers/proactivescaleup/README.md`

### Modified Files
- `pkg/controllers/provisioning/provisioner.go`
- `pkg/controllers/controllers.go`
- `pkg/operator/options/options.go`

## Testing

### Build Verification
```bash
go build ./pkg/...  # All packages build successfully
```

### Unit Tests
```bash
go test ./pkg/controllers/proactivescaleup/...  # Tests pass
```

## Benefits

1. **Faster Scale-Up**: Nodes are ready before pods are created
2. **Zero Kubernetes Overhead**: No actual pods created/deleted
3. **Transparent**: Fake pods not visible via kubectl
4. **Configurable**: Limits prevent resource exhaustion
5. **Safe**: Backward compatible, disabled by default

## References

- Azure Karpenter PR: https://github.com/Azure/karpenter-provider-azure/pull/1399
- Cluster Autoscaler PR: https://github.com/kubernetes/autoscaler/pull/7145

## Future Enhancements

Potential improvements for future iterations:
- Metrics for fake pod injection
- More sophisticated filtering logic
- Support for additional workload types (e.g., CronJobs)
- Dynamic adjustment of injection limits based on cluster size
