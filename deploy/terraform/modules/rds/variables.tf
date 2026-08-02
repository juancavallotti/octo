variable "name" {
  type        = string
  description = "Instance identifier, and the prefix for its subnet group and security group."
}

variable "vpc_id" {
  type        = string
  description = "VPC to create the security group in — the same VPC as the cluster."
}

variable "subnet_ids" {
  type        = list(string)
  description = "Subnets for the DB subnet group. Must span at least two availability zones, even for a single-AZ instance."
}

variable "client_security_group_id" {
  type        = string
  description = "Security group allowed to reach 5432 — the EKS node group's (module.eks.node_security_group_id). Pods inherit it through the VPC CNI, which is what lets the schema Job connect."
}

variable "engine_version" {
  type        = string
  description = "Postgres major version. Major-only lets RDS pick the current minor."
  default     = "16"
}

variable "instance_class" {
  type        = string
  description = "RDS instance class. db.t4g.micro is the cheapest that runs Postgres 16 and is ample for a test deployment."
  default     = "db.t4g.micro"
}

variable "allocated_storage_gb" {
  type        = number
  description = "Initial storage. 20 is the gp3 minimum on RDS."
  default     = 20
}

variable "max_allocated_storage_gb" {
  type        = number
  description = "Storage autoscaling ceiling. Set equal to allocated_storage_gb to disable growth."
  default     = 100
}

variable "database_name" {
  type        = string
  description = "Database the chart connects to."
  default     = "octo"
}

variable "database_user" {
  type        = string
  description = "Master user. It owns database_name, which the chart's schema Job needs: on Postgres 16 the CREATE privilege on schema `public` is revoked from PUBLIC, so a non-owner cannot create tables at all."
  default     = "octo"
}

variable "multi_az" {
  type        = bool
  description = "Synchronous standby in a second availability zone, at roughly double the cost."
  default     = false
}

variable "backup_retention_days" {
  type        = number
  description = "Days of automated backups. 0 disables them — right for an instance destroyed with its cluster, wrong for anything else."
  default     = 0
}

variable "skip_final_snapshot" {
  type        = bool
  description = "Skip the final snapshot on delete. True lets `terraform destroy` complete unattended; false is what you want for real data."
  default     = true
}

variable "deletion_protection" {
  type        = bool
  description = "Refuse to delete the instance. Off so destroy completes."
  default     = false
}

variable "apply_immediately" {
  type        = bool
  description = "Apply modifications at once rather than in the next maintenance window. Fine for a test instance; can cause downtime on a live one."
  default     = true
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to every resource here."
  default     = {}
}
