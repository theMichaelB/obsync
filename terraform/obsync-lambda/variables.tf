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

variable "obsidian_email" {
  description = "Obsidian account email"
  type        = string
}

variable "obsidian_password" {
  description = "Obsidian account password"
  type        = string
  sensitive   = true
}

variable "obsidian_totp_secret" {
  description = "Obsidian TOTP secret (BASE32)"
  type        = string
  sensitive   = true
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
