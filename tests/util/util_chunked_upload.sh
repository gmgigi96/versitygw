#!/usr/bin/env bash

attempt_seed_signature_without_content_length() {
  if [ "$#" -ne 3 ]; then
    log 2 "'attempt_seed_signature_without_content_length' requires bucket name, key, data file"
    return 1
  fi
  if ! result=$(COMMAND_LOG="$COMMAND_LOG" CONTENT_ENCODING="aws-chunked" BUCKET_NAME="$1" OBJECT_KEY="$2" DATA_FILE="$3" OUTPUT_FILE="$TEST_FILE_FOLDER/result.txt" ./tests/rest_scripts/put_object.sh); then
    log 2 "error putting object: $result"
    return 1
  fi
  if [ "$result" != 411 ]; then
    log 2 "expected '411', actual '$result' ($(cat "$TEST_FILE_FOLDER/result.txt"))"
    return 1
  fi
  return 0
}

attempt_chunked_upload_with_bad_first_signature() {
  if [ $# -ne 3 ]; then
    log 2 "'attempt_chunked_upload_with_bad_first_signature' requires data file, bucket name, key"
    return 1
  fi
  if ! result=$(COMMAND_LOG="$COMMAND_LOG" \
         AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
         AWS_ENDPOINT_URL="$AWS_ENDPOINT_URL" \
         DATA_FILE="$1" \
         BUCKET_NAME="$2" \
         OBJECT_KEY="$3" CHUNK_SIZE=8192 TEST_MODE=false COMMAND_FILE="$TEST_FILE_FOLDER/command.txt" FIRST_SIGNATURE="xxxxxxxx" ./tests/rest_scripts/put_object_openssl_chunked_example.sh 2>&1); then
    log 2 "error creating command: $result"
    return 1
  fi

  host="${AWS_ENDPOINT_URL#http*://}"
  if [ "$host" == "s3.amazonaws.com" ]; then
    host+=":443"
  fi
  if ! result=$(openssl s_client -connect "$host" -ign_eof < "$TEST_FILE_FOLDER/command.txt" 2>&1); then
    log 2 "error sending openssl command: $result"
    return 1
  fi
  response_code="$(echo "$result" | grep "HTTP" | awk '{print $2}')"
  log 5 "response code: $response_code"
  if [ "$response_code" != "403" ]; then
    log 2 "expected code '403', was '$response_code'"
    return 1
  fi
  response_data="$(echo "$result" | grep "<")"
  log 5 "response data: $response_data"
  log 5 "END"
  if ! check_xml_element <(echo "$response_data") "SignatureDoesNotMatch" "Error" "Code"; then
    log 2 "error checking XML element"
    return 1
  fi
  return 0
}

chunked_upload_success() {
  if [ $# -ne 3 ]; then
    log 2 "'chunked_upload_success_as' requires data file, bucket name, key"
    return 1
  fi
  if ! result=$(COMMAND_LOG="$COMMAND_LOG" \
         AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
         AWS_ENDPOINT_URL="$AWS_ENDPOINT_URL" \
         DATA_FILE="$1" \
         BUCKET_NAME="$2" \
         OBJECT_KEY="$3" CHUNK_SIZE=8192 TEST_MODE=false COMMAND_FILE="$TEST_FILE_FOLDER/command.txt" ./tests/rest_scripts/put_object_openssl_chunked_example.sh 2>&1); then
    log 2 "error creating command: $result"
    return 1
  fi

  host="${AWS_ENDPOINT_URL#http*://}"
  if [ "$host" == "s3.amazonaws.com" ]; then
    host+=":443"
  fi
  if ! result=$(openssl s_client -connect "$host" -ign_eof < "$TEST_FILE_FOLDER/command.txt" 2>&1); then
    log 2 "error sending openssl command: $result"
    return 1
  fi
  response_code="$(echo "$result" | grep "HTTP" | awk '{print $2}')"
  if [ "$response_code" != "200" ]; then
    log 2 "expected response '200', was '$response_code'"
    return 1
  fi
  return 0
}

attempt_chunked_upload_with_bad_final_signature() {
  if [ $# -ne 3 ]; then
    log 2 "'attempt_chunked_upload_with_bad_first_signature' requires data file, bucket name, key"
    return 1
  fi
  if ! result=$(COMMAND_LOG="$COMMAND_LOG" \
         AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
         AWS_ENDPOINT_URL="$AWS_ENDPOINT_URL" \
         DATA_FILE="$1" \
         BUCKET_NAME="$2" \
         OBJECT_KEY="$3" \
         CHUNK_SIZE=8192 \
         TEST_MODE=false \
         COMMAND_FILE="$TEST_FILE_FOLDER/command.txt" \
         FINAL_SIGNATURE="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" ./tests/rest_scripts/put_object_openssl_chunked_example.sh 2>&1); then
    log 2 "error creating command: $result"
    return 1
  fi
  host="${AWS_ENDPOINT_URL#http*://}"
  if [ "$host" == "s3.amazonaws.com" ]; then
    host+=":443"
  fi
  if ! result=$(openssl s_client -connect "$host" -ign_eof < "$TEST_FILE_FOLDER/command.txt" 2>&1); then
    log 2 "error sending openssl command: $result"
    return 1
  fi
  response_code="$(echo "$result" | grep "HTTP" | awk '{print $2}')"
  log 5 "response code: $response_code"
  if [ "$response_code" != "403" ]; then
    log 2 "expected code '403', was '$response_code'"
    return 1
  fi
  response_data="$(echo "$result" | grep "<")"
  log 5 "response data: $response_data"
  log 5 "END"
  if ! check_xml_element <(echo "$response_data") "SignatureDoesNotMatch" "Error" "Code"; then
    log 2 "error checking XML element"
    return 1
  fi
  return 0
}
