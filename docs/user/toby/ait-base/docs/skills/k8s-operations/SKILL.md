---
name: k8s-operations
description: Best practices for Kubernetes and Kustomize operations, emphasizing diffing and correct root context.
---

# Kubernetes Operations

Guidelines and workflows for applying changes to Kubernetes clusters, specifically when dealing with `kustomize` manifests.

## When to Activate

- Modifying or interacting with Kubernetes manifests (`.yaml` files).
- Applying changes to a Kubernetes cluster.
- Navigating and debugging Kustomize directory structures.

## Core Principles

### 1. Never Apply Component Manifests Directly

When a `.yaml` file is located in a folder with a `kustomization.yaml` file, and its filename is referenced within that `kustomization.yaml`:
- **DO NOT** execute `kubectl apply -f <file.yaml>` directly.
- **DO** analyze the directory structure to find the correct **root** `kustomization.yaml` (usually an environment or overlay root) that manages that component.

### 2. The Diff-Apply-Diff Workflow

Always verify the exact changes that will be executed on the cluster before applying them, and confirm the state is fully reconciled afterward.

When working with `kubectl -k`:

1. **Pre-flight Diff**: Diff against the root kustomize file to verify the expected difference.
   ```bash
   kubectl diff -k <path-to-root-kustomize-dir>
   ```
2. **Apply**: Apply the root kustomize configuration.
   ```bash
   kubectl apply -k <path-to-root-kustomize-dir>
   ```
3. **Post-flight Verification**: Diff again to ensure no remaining differences exist.
   ```bash
   kubectl diff -k <path-to-root-kustomize-dir>
   ```

### 3. Kustomize-Specific Workflow

If your environment or tooling supports native `kustomize` application commands, follow this exact sequence:

1. **Check Expected Changes**: Run `kustomize diff` to preview the differences.
2. **Apply Changes**: Run `kubectl apply` (or `kustomize apply` if aliased/available in your environment).
3. **Verify Reconciliation**: Run `kubectl diff` immediately after to ensure the cluster has fully reconciled and there are no lingering differences.
