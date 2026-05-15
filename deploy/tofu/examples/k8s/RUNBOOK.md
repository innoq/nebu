# Nebu — Kubernetes / Helm Runbook

This runbook covers deploying, upgrading, rolling back, and tearing down Nebu on a local
[kind](https://kind.sigs.k8s.io/) cluster for development and smoke-testing, as well as
configuring horizontal pod autoscaling.

For production deployments replace the `kind` steps with your cluster setup procedure.

---

## Prerequisites

The following tools must be available on your workstation:

| Tool | Minimum version | Install |
|---|---|---|
| [kind](https://kind.sigs.k8s.io/) | 0.22+ | `brew install kind` |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | 1.28+ | `brew install kubectl` |
| [Helm](https://helm.sh/) | 3.14+ | `brew install helm` |
| [OpenTofu](https://opentofu.org/) | 1.6+ | `brew install opentofu` |

Verify installations:

```bash
kind version
kubectl version --client
helm version
tofu version
```

---

## 1. Create a local kind cluster

```bash
kind create cluster --name nebu-dev
```

Verify the cluster is running:

```bash
kubectl cluster-info --context kind-nebu-dev
```

---

## 2. Deploy with OpenTofu

```bash
cd deploy/tofu/examples/k8s

# Initialise providers (no backend credentials needed for local kind)
tofu init -backend=false

# Review what will be created
tofu plan \
  -var="gateway_image_tag=dev" \
  -var="core_image_tag=dev"

# Apply
tofu apply \
  -var="gateway_image_tag=dev" \
  -var="core_image_tag=dev"
```

### Optional: enable Ingress

```bash
tofu apply \
  -var="gateway_image_tag=dev" \
  -var="core_image_tag=dev" \
  -var="ingress_enabled=true"
```

---

## 3. Smoke test — verify pods are Running

After `tofu apply` completes, all Nebu pods should reach `Running` state within 3 minutes:

```bash
kubectl wait --namespace nebu \
  --for=condition=Ready pod \
  --selector=app.kubernetes.io/instance=nebu \
  --timeout=180s
```

Check pod status manually:

```bash
kubectl get pods -n nebu
```

Expected output (all pods `Running`):

```
NAME                            READY   STATUS    RESTARTS   AGE
nebu-gateway-...                1/1     Running   0          2m
nebu-core-...                   1/1     Running   0          2m
```

---

## 4. Direct Helm install (without OpenTofu)

For quick iteration during development you can install or upgrade the chart directly:

```bash
helm install nebu deploy/helm/nebu/ \
  -f deploy/helm/nebu/values-dev.yaml \
  --set gateway.image.tag=dev \
  --set core.image.tag=dev \
  --namespace nebu \
  --create-namespace
```

---

## 5. Helm upgrade

To update a running deployment with a new image tag:

```bash
helm upgrade nebu deploy/helm/nebu/ \
  -f deploy/helm/nebu/values-dev.yaml \
  --set gateway.image.tag=<NEW_TAG> \
  --set core.image.tag=<NEW_TAG> \
  --namespace nebu
```

Verify the rollout:

```bash
kubectl rollout status deployment/nebu-gateway -n nebu
kubectl rollout status deployment/nebu-core -n nebu
```

---

## 6. Rollback

List Helm revision history:

```bash
helm history nebu -n nebu
```

Roll back to the previous revision:

```bash
helm rollback nebu 0 -n nebu
```

Roll back to a specific revision number:

```bash
helm rollback nebu <REVISION> -n nebu
```

---

## 7. HPA — Horizontal Pod Autoscaling

### Via Helm values (recommended)

Enable HPA in `values.yaml` or via `--set`:

```bash
helm upgrade nebu deploy/helm/nebu/ \
  -f deploy/helm/nebu/values-dev.yaml \
  --set autoscaling.gateway.enabled=true \
  --set autoscaling.gateway.minReplicas=1 \
  --set autoscaling.gateway.maxReplicas=5 \
  --set autoscaling.gateway.targetCPUUtilizationPercentage=70 \
  -n nebu
```

### Via kubectl (imperative — for quick testing only)

```bash
kubectl autoscale deployment nebu-gateway \
  --cpu-percent=70 \
  --min=1 \
  --max=5 \
  -n nebu
```

Verify:

```bash
kubectl get hpa -n nebu
```

> Note: `kubectl autoscale` creates an HPA outside of Helm state. Prefer the Helm values
> approach for persistent configuration. Remove the kubectl-created HPA before the next
> `helm upgrade` to avoid conflicts.

---

## 8. Teardown

### Destroy via OpenTofu

```bash
cd deploy/tofu/examples/k8s
tofu destroy \
  -var="gateway_image_tag=dev" \
  -var="core_image_tag=dev"
```

### Delete the kind cluster

```bash
kind delete cluster --name nebu-dev
```

This removes the entire cluster and all resources within it.

---

## Troubleshooting

| Symptom | Command | Notes |
|---|---|---|
| Pods stuck in `Pending` | `kubectl describe pod <name> -n nebu` | Check resource limits and node capacity |
| Image pull errors | `kubectl describe pod <name> -n nebu` | Verify `image_registry` + tag; `imagePullPolicy: IfNotPresent` is default for dev |
| CrashLoopBackOff | `kubectl logs <name> -n nebu --previous` | Check env vars and secrets |
| Helm release failed | `helm status nebu -n nebu` | See `helm history nebu -n nebu` for details |
| HPA shows `<unknown>` CPU | `kubectl top nodes` | Ensure `metrics-server` is installed in the cluster |
