terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

resource "random_id" "bucket_suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "obsync" {
  bucket = "obsync-${random_id.bucket_suffix.hex}"
}

resource "aws_s3_bucket_versioning" "obsync" {
  bucket = aws_s3_bucket.obsync.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "obsync" {
  bucket = aws_s3_bucket.obsync.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}

data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda_role" {
  name               = "obsync-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.trust.json
}

data "aws_iam_policy_document" "lambda_policy" {
  statement {
    effect = "Allow"
    actions = [
      "s3:GetObject", "s3:PutObject", "s3:DeleteObject",
      "s3:ListBucket", "s3:GetObjectVersion", "s3:PutObjectAcl"
    ]
    resources = [aws_s3_bucket.obsync.arn, "${aws_s3_bucket.obsync.arn}/*"]
  }
  statement {
    effect = "Allow"
    actions = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["arn:aws:logs:*:*:*"]
  }
  statement {
    effect = "Allow"
    actions = ["secretsmanager:GetSecretValue"]
    resources = [var.secrets_manager_secret_arn]
  }
}

resource "aws_iam_policy" "lambda_policy" {
  name   = "obsync-lambda-policy"
  policy = data.aws_iam_policy_document.lambda_policy.json
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_attach" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = aws_iam_policy.lambda_policy.arn
}

# CloudWatch Log Group with 1-day retention
resource "aws_cloudwatch_log_group" "lambda_logs" {
  name              = "/aws/lambda/${var.function_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "obsync" {
  filename         = var.lambda_zip_path
  function_name    = var.function_name
  role             = aws_iam_role.lambda_role.arn
  handler          = "bootstrap"
  source_code_hash = filebase64sha256(var.lambda_zip_path)
  runtime          = "provided.al2"
  architectures    = ["arm64"]
  timeout          = var.timeout
  memory_size      = var.memory_size
  
  depends_on = [aws_cloudwatch_log_group.lambda_logs]

  environment {
    variables = {
      OBSYNC_SECRET_NAME      = var.secrets_manager_secret_arn
      S3_BUCKET               = aws_s3_bucket.obsync.id
      S3_PREFIX               = var.s3_prefix
      S3_STATE_PREFIX         = var.s3_state_prefix
      DOWNLOAD_ON_STARTUP     = "true"
      LOG_LEVEL               = var.log_level
      OBSYNC_DEBUG            = var.enable_debug ? "true" : "false"
      SAVE_WEBSOCKET_TRACE    = var.save_websocket_trace ? "true" : "false"
      WEBSOCKET_TRACE_BUCKET  = var.save_websocket_trace ? aws_s3_bucket.obsync.id : ""
      WEBSOCKET_TRACE_PREFIX  = var.save_websocket_trace ? "debug/websocket-traces/" : ""
      OBSYNC_MAX_CONCURRENT   = tostring(var.max_concurrent)
      OBSYNC_CHUNK_SIZE_MB    = tostring(var.chunk_size_mb)
    }
  }
}

resource "aws_cloudwatch_event_rule" "schedule" {
  count               = var.enable_schedule ? 1 : 0
  name                = "obsync-schedule"
  description         = "Trigger obsync on a schedule"
  schedule_expression = var.schedule_expression
}

resource "aws_cloudwatch_event_target" "lambda" {
  count     = var.enable_schedule ? 1 : 0
  rule      = aws_cloudwatch_event_rule.schedule[0].name
  target_id = "obsync-target"
  arn       = aws_lambda_function.obsync.arn
  input     = jsonencode({ action = "sync", sync_type = "incremental" })
}

resource "aws_lambda_permission" "allow_cloudwatch" {
  count         = var.enable_schedule ? 1 : 0
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.obsync.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.schedule[0].arn
}

# Note: Secrets Manager access is already granted in the lambda_policy above
