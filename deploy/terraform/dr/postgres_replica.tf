// Cross-cloud Postgres read replica concept: CloudSQL -> RDS.
//
// In production this would use a third-party replication tool
// (e.g. Bucardo, pglogical, or managed cross-cloud replication)
// since CloudSQL and RDS cannot natively replicate to each other.
// This Terraform defines the RDS target instance that would
// receive the replicated data.

variable "db_instance_class" {
  description = "RDS instance class for the DR Postgres replica"
  type        = string
  default     = "db.t3.medium"
}

variable "db_allocated_storage" {
  description = "Allocated storage (GB) for the DR Postgres replica"
  type        = number
  default     = 50
}

variable "db_engine_version" {
  description = "PostgreSQL engine version for RDS"
  type        = string
  default     = "16.4"
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "agentic_ecommerce"
}

variable "db_username" {
  description = "Master username for the RDS instance"
  type        = string
  default     = "ecommerce_admin"
  sensitive   = true
}

variable "db_password" {
  description = "Master password for the RDS instance"
  type        = string
  sensitive   = true
}

variable "vpc_id" {
  description = "VPC ID for the DR environment"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs for the RDS subnet group"
  type        = list(string)
}

resource "aws_db_subnet_group" "dr" {
  name       = "agentic-ecommerce-dr-db"
  subnet_ids = var.private_subnet_ids

  tags = {
    Name        = "agentic-ecommerce-dr-db-subnet"
    environment = "dr"
  }
}

resource "aws_security_group" "rds" {
  name_prefix = "agentic-ecommerce-dr-rds-"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["10.1.0.0/16"]
    description = "PostgreSQL from DR VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "agentic-ecommerce-dr-rds-sg"
  }
}

resource "aws_db_instance" "dr_replica" {
  identifier     = "agentic-ecommerce-dr"
  engine         = "postgres"
  engine_version = var.db_engine_version
  instance_class = var.db_instance_class

  allocated_storage     = var.db_allocated_storage
  max_allocated_storage = var.db_allocated_storage * 2
  storage_encrypted     = true

  db_name  = var.db_name
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.dr.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  multi_az                  = true
  backup_retention_period   = 7
  skip_final_snapshot       = false
  final_snapshot_identifier = "agentic-ecommerce-dr-final"

  tags = {
    Name        = "agentic-ecommerce-dr-postgres"
    environment = "dr"
    role        = "replica-target"
  }
}

output "dr_postgres_endpoint" {
  description = "RDS endpoint for the DR Postgres instance"
  value       = aws_db_instance.dr_replica.endpoint
}
