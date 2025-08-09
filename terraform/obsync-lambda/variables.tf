variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "function_name" {
  description = "Lambda function name"
  type        = string
  default     = "obsync-sync"
}

variable "lambda_zip_path" {
  description = "Path to Lambda deployment ZIP"
  type        = string
  default     = "../../build/obsync-lambda.zip"
}

variable "memory_size" {
  description = "Lambda memory size (MB)"
  type        = number
  default     = 1024
}

variable "timeout" {
  description = "Lambda timeout (seconds)"
  type        = number
  default     = 900
}

variable "enable_schedule" {
  description = "Enable CloudWatch Events schedule"
  type        = bool
  default     = false
}

variable "schedule_expression" {
  description = "CloudWatch schedule expression"
  type        = string
  default     = "rate(1 hour)"
}

variable "s3_prefix" {
  description = "Prefix for vault objects"
  type        = string
  default     = "vaults/"
}

variable "s3_state_prefix" {
  description = "Prefix for state objects"
  type        = string
  default     = "state/"
}

variable "secrets_manager_secret_arn" {
  description = "ARN of Secrets Manager secret containing combined credentials JSON (required)"
  type        = string
  
  validation {
    condition     = length(var.secrets_manager_secret_arn) > 0
    error_message = "The secrets_manager_secret_arn must be provided. Create a secret with: aws secretsmanager create-secret --name obsync-credentials --secret-string file://credentials.json"
  }
}

variable "enable_debug" {
  description = "Enable debug logging"
  type        = bool
  default     = false
}

variable "save_websocket_trace" {
  description = "Save WebSocket trace data to S3 for debugging"
  type        = bool
  default     = false
}

variable "log_level" {
  description = "Logging level (debug, info, warn, error)"
  type        = string
  default     = "info"
  
  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "log_level must be one of: debug, info, warn, error"
  }
}

variable "max_concurrent" {
  description = "Maximum concurrent file operations (default 50 for Lambda)"
  type        = number
  default     = 50
}

variable "chunk_size_mb" {
  description = "File chunk size in MB for downloads (default 10)"
  type        = number
  default     = 10
}
