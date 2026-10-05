variable "region" {
  type = string
}

variable "broker_count" {
  type    = number
  default = 3
}

resource "aws_msk_cluster" "nidaw_kafka" {
  cluster_name           = "nidaw-kafka-${var.region}"
  kafka_version          = "3.5.1"
  number_of_broker_nodes = var.broker_count

  broker_node_group_info {
    instance_type   = "kafka.m5.large"
    storage_info {
      ebs_storage_info {
        volume_size = 1000
      }
    }
  }

  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
    }
  }

  logging_info {
    broker_logs {
      s3 {
        enabled = true
        bucket  = aws_s3_bucket.kafka_logs.id
        prefix  = "logs/"
      }
    }
  }
}

resource "aws_msk_configuration" "nidaw_kafka_config" {
  name              = "nidaw-kafka-config"
  kafka_versions    = ["3.5.1"]
  server_properties = <<PROPERTIES
auto.create.topics.enable = true
delete.topic.enable = true
log.retention.hours = 168
log.segment.bytes = 1073741824
PROPERTIES
}