# An RDS for PostgreSQL instance inside the EKS cluster's VPC, plus the security
# group that lets the cluster's nodes — and therefore its pods — reach it.
#
# A single aws_db_instance, not Aurora: Aurora's smallest usable shape is a
# db.t4g.medium writer on top of a cluster resource, roughly triple the cost, to
# exercise nothing this chart does differently. Set multi_az for real redundancy.

resource "random_password" "octo" {
  length = 24
  # Alphanumeric only: the password is interpolated into a postgres:// DSN, where
  # special characters would need percent-encoding. RDS separately rejects '/',
  # '@', '"' and spaces in a master password, so this avoids two problems at once.
  special = false
}

resource "aws_db_subnet_group" "this" {
  name       = var.name
  subnet_ids = var.subnet_ids
  tags       = var.tags
}

resource "aws_security_group" "this" {
  name_prefix = "${var.name}-rds-"
  description = "Postgres access for the octo cluster"
  vpc_id      = var.vpc_id
  tags        = var.tags

  lifecycle {
    create_before_destroy = true
  }
}

# Security-group-to-security-group, not a CIDR. Pods use the AWS VPC CNI, so a pod
# ENI carries the node group's security group — which means allowing that group is
# what actually lets the schema Job connect, and it keeps working when the node
# subnets change.
resource "aws_vpc_security_group_ingress_rule" "postgres" {
  security_group_id            = aws_security_group.this.id
  description                  = "Postgres from the cluster nodes"
  referenced_security_group_id = var.client_security_group_id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
}

resource "aws_db_instance" "this" {
  identifier     = var.name
  engine         = "postgres"
  engine_version = var.engine_version
  instance_class = var.instance_class

  # 20GB is the gp3 minimum on RDS. Autoscaling grows it; it can never shrink.
  allocated_storage     = var.allocated_storage_gb
  max_allocated_storage = var.max_allocated_storage_gb
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = var.database_name
  username = var.database_user
  password = random_password.octo.result

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.this.id]
  publicly_accessible    = false
  multi_az               = var.multi_az

  # RDS Postgres sets rds.force_ssl=1 in its default parameter group, which the
  # chart's sslmode=require satisfies without needing the RDS CA bundle in every
  # pod (verify-full would).

  # Test-environment settings, all four of which are wrong for anything you keep.
  skip_final_snapshot     = var.skip_final_snapshot
  deletion_protection     = var.deletion_protection
  backup_retention_period = var.backup_retention_days
  apply_immediately       = var.apply_immediately

  auto_minor_version_upgrade = true
  tags                       = var.tags
}
