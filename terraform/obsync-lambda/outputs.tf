output "function_name" {
  description = "Deployed Lambda function name"
  value       = aws_lambda_function.obsync.function_name
}

output "s3_bucket" {
  description = "S3 bucket for vaults and state"
  value       = aws_s3_bucket.obsync.id
}
