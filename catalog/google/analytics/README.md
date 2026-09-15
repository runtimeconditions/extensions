# Google Analytics extension

This experimental extension defines the requirement made by a web workload that
sends analytics events to Google Analytics. It intentionally does not place a
Google Analytics measurement ID in a Profile. The Profile declares the logical
`measurementId` configuration property and the environment-variable name from
which the target environment supplies its value.

The first immutable candidate release is
[`0.1.0`](releases/0.1.0/runtimeconditions.extension.yaml).

Example Condition:

```yaml
- name: site-analytics
  kind: google.analytics
  interface:
    type: web
    events:
      - page_view
  configuration:
    env:
      - property: measurementId
        name: GA_MEASUREMENT_ID
```
