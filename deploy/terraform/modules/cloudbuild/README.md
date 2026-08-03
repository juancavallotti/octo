# `cloudbuild`

Cloud Build trigger and its IAM.

A trigger on version tags, the Artifact Registry writer binding, and — when
`enable_deploy` is set — the extra permissions the build needs to roll the cluster
itself (state bucket access, instance admin, IAP tunnel, service account user).

**Used by:** infra

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
