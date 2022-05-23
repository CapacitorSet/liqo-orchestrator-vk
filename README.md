# liqo-orchestrator-vk

This repository is a fork of Liqo meant to be used with [liqo-broker](https://github.com/CapacitorSet/liqo-broker). It patches the virtual kubelet to support an orchestrator broker.

## Patches

The virtual kubelet is patched to set and propagate the labels `brokering.liqo.io/root-ip` and `brokering.liqo.io/root-cid`, containing respectively the pod IP on the final cluster and its cluster ID. The broker (or in general the penultimate cluster in the NamespaceOffloading daisy chain) sets these labels by reading the information directly, while the other clusters propagate the labels by copying them upstream.

These labels are then used in IP address translation as parameters to the IPAM call. The end result is that we can now have a decoupling of the orchestration and the data plane: a customer can offload their pods on a cluster A, while routing traffic towards a cluster B.

## Building

```sh
docker build --build-arg COMPONENT=virtual-kubelet -t capacitorset/liqo-orchestrator-vk -f build/common/Dockerfile .
```
