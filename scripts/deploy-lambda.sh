#!/usr/bin/env bash
set -euo pipefail

# Simple Lambda deploy/update helper for obsync
# Usage:
#   ./scripts/deploy-lambda.sh \
#     --function obsync-sync \
#     --role arn:aws:iam::<ACCOUNT_ID>:role/obsync-lambda-role \
#     --region us-east-1 \
#     --env KEY=VALUE [--env KEY=VALUE ...]

FUNC="obsync-sync"
ROLE=""
REGION="${AWS_REGION:-us-east-1}"
ZIP="build/obsync-lambda.zip"
ENV_VARS=()
TIMEOUT=900
MEMORY=1024

while [[ $# -gt 0 ]]; do
  case "$1" in
    --function) FUNC="$2"; shift 2;;
    --role) ROLE="$2"; shift 2;;
    --region) REGION="$2"; shift 2;;
    --zip) ZIP="$2"; shift 2;;
    --timeout) TIMEOUT="$2"; shift 2;;
    --memory) MEMORY="$2"; shift 2;;
    --env) ENV_VARS+=("$2"); shift 2;;
    *) echo "Unknown arg: $1"; exit 1;;
  esac
done

if [[ ! -f "$ZIP" ]]; then
  echo "[$ZIP] not found. Building..."
  make build-lambda
fi

ENV_JSON=$(printf '{%s}' "$(IFS=, ; echo "${ENV_VARS[*]}")")

set +e
aws --region "$REGION" lambda get-function --function-name "$FUNC" >/dev/null 2>&1
EXISTS=$?
set -e

if [[ $EXISTS -eq 0 ]]; then
  echo "Updating Lambda function: $FUNC"
  aws --region "$REGION" lambda update-function-code \
    --function-name "$FUNC" \
    --zip-file "fileb://$ZIP" >/dev/null

  aws --region "$REGION" lambda update-function-configuration \
    --function-name "$FUNC" \
    --timeout "$TIMEOUT" \
    --memory-size "$MEMORY" \
    ${ENV_VARS:+--environment "Variables=$ENV_JSON"} >/dev/null
else
  if [[ -z "$ROLE" ]]; then
    echo "--role is required for first-time creation" >&2
    exit 1
  fi
  echo "Creating Lambda function: $FUNC"
  aws --region "$REGION" lambda create-function \
    --function-name "$FUNC" \
    --runtime provided.al2 \
    --architecture arm64 \
    --role "$ROLE" \
    --handler bootstrap \
    --zip-file "fileb://$ZIP" \
    --timeout "$TIMEOUT" \
    --memory-size "$MEMORY" \
    ${ENV_VARS:+--environment "Variables=$ENV_JSON"} >/dev/null
fi

echo "Done. Function: $FUNC (region: $REGION)"
