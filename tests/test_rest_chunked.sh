#!/usr/bin/env bats

# Copyright 2024 Versity Software
# This file is licensed under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
#   http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

load ./bats-support/load
load ./bats-assert/load

source ./tests/logger.sh
source ./tests/setup.sh
source ./tests/util/util_bucket.sh
source ./tests/util/util_chunked_upload.sh
source ./tests/util/util_file.sh
source ./tests/util/util_head_object.sh
source ./tests/util/util_setup.sh

@test "REST - chunked upload, no content length" {
  if [ "$DIRECT" != "true" ]; then
    skip "https://github.com/versity/versitygw/issues/1043"
  fi
  run setup_bucket_and_file "$BUCKET_ONE_NAME" "$test_file"
  assert_success

  run attempt_seed_signature_without_content_length "$BUCKET_ONE_NAME" "$test_file" "$TEST_FILE_FOLDER/$test_file"
  assert_success
}

@test "REST - chunked upload, signature error" {
  if [ "$DIRECT" != "true" ]; then
    skip "https://github.com/versity/versitygw/issues/1123"
  fi
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  test_file="test-file"
  run create_test_file "$test_file" 8192
  assert_success

  run attempt_chunked_upload_with_bad_first_signature "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}

@test "REST - chunked upload, final signature error" {
  if [ "$DIRECT" != "true" ]; then
    skip "https://github.com/versity/versitygw/issues/1147"
  fi
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  test_file="test-file"
  run create_test_file "$test_file" 0
  assert_success

  run attempt_chunked_upload_with_bad_final_signature "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}

@test "REST - chunked upload, success (file with just a's)" {
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  sleep 10

  test_file="test-file"
  run create_file_single_char "$test_file" 8192 'a'
  assert_success

  run chunked_upload_success "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}

@test "REST - chunked upload, success (null bytes)" {
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  sleep 10

  test_file="test-file"
  run create_file_single_char "$test_file" 8192 '\0'
  assert_success

  run chunked_upload_success "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}

@test "REST - chunked upload, success (random bytes)" {
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  sleep 10

  test_file="test-file"
  run create_test_file "$test_file" 10000
  assert_success

  run chunked_upload_success "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}

@test "REST - chunked upload, success (zero-byte file)" {
  run setup_bucket "s3api" "$BUCKET_ONE_NAME"
  assert_success

  sleep 10

  test_file="test-file"
  run create_test_file "$test_file" 0
  assert_success

  run chunked_upload_success "$TEST_FILE_FOLDER/$test_file" "$BUCKET_ONE_NAME" "$test_file"
  assert_success
}
