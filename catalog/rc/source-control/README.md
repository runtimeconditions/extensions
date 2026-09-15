# Source Control extension

This experimental extension describes Git repository access required by a
workload such as a development container or CI job. Repository credentials and
concrete repository locations remain target-environment configuration and are
not embedded in the Profile.

The first immutable candidate release is
[`0.1.0`](releases/0.1.0/runtimeconditions.extension.yaml).

Example Condition:

```yaml
- name: application-source
  kind: source_control
  interface:
    type: git
    provider: github
    access:
      - fetch
      - push
```
