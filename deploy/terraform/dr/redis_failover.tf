// Cross-cloud Redis failover concept: GCP Memorystore -> ElastiCache.
//
// The DR Redis instance runs in warm standby. Application-level
// cache warming occurs during failover via the cache preloader
// (deferred to production readiness).

variable "redis_node_type" {
  description = "ElastiCache node type for the DR Redis cluster"
  type        = string
  default     = "cache.t3.medium"
}

variable "redis_num_replicas" {
  description = "Number of read replicas in the ElastiCache replication group"
  type        = number
  default     = 1
}

variable "redis_subnet_group_subnet_ids" {
  description = "Subnet IDs for the ElastiCache subnet group"
  type        = list(string)
}

variable "redis_vpc_id" {
  description = "VPC ID for the DR Redis security group"
  type        = string
}

resource "aws_elasticache_subnet_group" "dr" {
  name       = "agentic-ecommerce-dr-redis"
  subnet_ids = var.redis_subnet_group_subnet_ids
}

resource "aws_security_group" "redis" {
  name_prefix = "agentic-ecommerce-dr-redis-"
  vpc_id      = var.redis_vpc_id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = ["10.1.0.0/16"]
    description = "Redis from DR VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "agentic-ecommerce-dr-redis-sg"
  }
}

resource "aws_elasticache_replication_group" "dr" {
  replication_group_id = "agentic-ecommerce-dr"
  description          = "DR Redis cluster for agentic-ecommerce"

  node_type            = var.redis_node_type
  num_cache_clusters   = var.redis_num_replicas + 1
  port                 = 6379

  subnet_group_name    = aws_elasticache_subnet_group.dr.name
  security_group_ids   = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true

  automatic_failover_enabled = true

  tags = {
    Name        = "agentic-ecommerce-dr-redis"
    environment = "dr"
    role        = "warm-standby"
  }
}

output "dr_redis_endpoint" {
  description = "ElastiCache primary endpoint for the DR Redis cluster"
  value       = aws_elasticache_replication_group.dr.primary_endpoint_address
}
