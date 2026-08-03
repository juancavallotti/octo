# Remote state in S3, keeping the AWS root's state in AWS. Not local state: losing
# it orphans a $0.10/hour control plane, a VPC and possibly an RDS instance, with
# nothing left able to destroy them.
#
# use_lockfile is native S3 conditional-write locking — no DynamoDB table, so the
# whole bootstrap is one `task eks:state:bucket`. It needs Terraform >= 1.10, which
# versions.tf pins.
#
# Bucket and region come from ../backend-aws.hcl at init time, because a backend
# block cannot reference variables; the key is a literal because it identifies this
# root and must not be shared with another.
terraform {
  backend "s3" {
    key          = "eks/terraform.tfstate"
    use_lockfile = true
  }
}
