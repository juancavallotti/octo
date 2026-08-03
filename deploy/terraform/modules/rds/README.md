# `rds`

RDS for PostgreSQL inside the cluster's VPC.

Instance, subnet group, and a security group that allows 5432 from the EKS node
group's security group — group-to-group rather than a CIDR, because pods inherit
the node security group through the VPC CNI, so that is what actually lets the
schema Job connect.

A single `aws_db_instance`, not Aurora: Aurora's smallest usable shape is roughly
triple the cost and exercises nothing this chart does differently.

Same PostgreSQL 16 ownership caveat as `../cloudsql` — the master user owns the
database, and must, for the schema Job to create tables.

**Used by:** eks

Inputs and outputs are documented on the `variable` and `output` blocks in
`variables.tf` and `outputs.tf`.
