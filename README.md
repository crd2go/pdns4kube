# pdns4kube

A Kubernetes operator that manages DNS zones provisioned by an [PowerDNS](https://www.powerdns.com/) Authoritative Server.
It watches `DNSZone` and `DNSRecord` resources (`pdns.example.io/v1`) and synchronizes zone create/update/delete
with the PowerDNS Authoritative API.

## Prerequisites

- [Go](https://go.dev/)
- [Docker](https://www.docker.com/)
- [Kind](https://kind.sigs.k8s.io/)
- [kubectl](https://kubernetes.io/docs/reference/kubectl/)
- [dig](https://bind9.readthedocs.io/) (from BIND, for verifying DNS)
- [Mage](https://magefile.org/)

Alternatively, if you use [Nix](https://nixos.org/), you can obtain the dev tools
with `nix develop` instead of installing them individually.

## Quick start

Start everything (Kind cluster, CRDs, a dev PowerDNS, API port-forward, and the operator).

With Nix, enter the dev shell:
```sh
nix develop
```
Without Nix, install Go and mage.

Then, from the resulting dev shell, run:

```sh
mage run
```

This creates a Kind cluster named `pdns4kube`, installs the CRD, deploys a dev PowerDNS,
forwards its API to `http://localhost:18081`, forwards its DNS (TCP) to `localhost:1053`,
and runs the operator locally. The operator is configured via the `PDNS_API_URL` and
`PDNS_API_KEY` environment variables (set automatically by `mage run`).

While `mage run` is running, create a zone:

```sh
kubectl apply -f demo/dnszone.yaml
```

This creates a `DNSZone` for `example.org` with two NS records (`ns1`/`ns2`).

Verify the zone exists in Kubernetes and that PowerDNS serves the NS records:

```sh
kubectl get pdns
dig +short +tcp @127.0.0.1 -p 1053 example.org NS
```

Add DNS records:

```sh
kubectl apply -f demo/records.yaml
```

Werify the DNS record:

```sh
kubectl get pdns
dig +short +tcp @127.0.0.1 -p 1053 www.example.org
```

Use `kubectl delete -f ...` or `kubectl delete <resource-type> <resource-name>` as usual to remove.

## Cleanup

- `mage pdnsDown` — removes the dev PowerDNS resources but keeps the Kind cluster.
- `mage kindDown` — deletes the whole Kind cluster.

## Notes

- The dev API key `pdns4kube-dev-key` is for local development only and is not secure.
- When running the operator manually, configure it with:
  - `PDNS_API_URL` — PowerDNS API endpoint (e.g. `http://127.0.0.1:18081`)
  - `PDNS_API_KEY` — PowerDNS API key
