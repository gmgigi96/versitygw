// Copyright 2023 Versity Software
// This file is licensed under the Apache License, Version 2.0
// (the "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package s3proxy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gmgigi96/versitygw/auth"
	"github.com/gmgigi96/versitygw/backend"
	"github.com/gmgigi96/versitygw/s3err"
	"github.com/gmgigi96/versitygw/s3response"
)

const aclKey string = "versitygwAcl"

type S3Proxy struct {
	backend.BackendUnsupported

	client *s3.Client

	access          string
	secret          string
	endpoint        string
	awsRegion       string
	disableChecksum bool
	sslSkipVerify   bool
	debug           bool
}

var _ backend.Backend = &S3Proxy{}

func New(access, secret, endpoint, region string, disableChecksum, sslSkipVerify, debug bool) (*S3Proxy, error) {
	s := &S3Proxy{
		access:          access,
		secret:          secret,
		endpoint:        endpoint,
		awsRegion:       region,
		disableChecksum: disableChecksum,
		sslSkipVerify:   sslSkipVerify,
		debug:           debug,
	}
	client, err := s.getClientWithCtx(context.Background())
	if err != nil {
		return nil, err
	}
	s.client = client
	return s, nil
}

func (s *S3Proxy) ListBuckets(ctx context.Context, input s3response.ListBucketsInput) (s3response.ListAllMyBucketsResult, error) {
	output, err := s.client.ListBuckets(ctx, &s3.ListBucketsInput{
		ContinuationToken: &input.ContinuationToken,
		MaxBuckets:        &input.MaxBuckets,
		Prefix:            &input.Prefix,
	})
	if err != nil {
		return s3response.ListAllMyBucketsResult{}, handleError(err)
	}

	var buckets []s3response.ListAllMyBucketsEntry
	for _, b := range output.Buckets {
		buckets = append(buckets, s3response.ListAllMyBucketsEntry{
			Name:         *b.Name,
			CreationDate: *b.CreationDate,
		})
	}

	return s3response.ListAllMyBucketsResult{
		Owner: s3response.CanonicalUser{
			ID: *output.Owner.ID,
		},
		Buckets: s3response.ListAllMyBucketsList{
			Bucket: buckets,
		},
		ContinuationToken: backend.GetStringFromPtr(output.ContinuationToken),
		Prefix:            backend.GetStringFromPtr(output.Prefix),
	}, nil
}

func (s *S3Proxy) HeadBucket(ctx context.Context, input *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	out, err := s.client.HeadBucket(ctx, input)
	return out, handleError(err)
}

func (s *S3Proxy) CreateBucket(ctx context.Context, input *s3.CreateBucketInput, acl []byte) error {
	if input.GrantFullControl != nil && *input.GrantFullControl == "" {
		input.GrantFullControl = nil
	}
	if input.GrantRead != nil && *input.GrantRead == "" {
		input.GrantRead = nil
	}
	if input.GrantReadACP != nil && *input.GrantReadACP == "" {
		input.GrantReadACP = nil
	}
	if input.GrantWrite != nil && *input.GrantWrite == "" {
		input.GrantWrite = nil
	}
	if input.GrantWriteACP != nil && *input.GrantWriteACP == "" {
		input.GrantWriteACP = nil
	}
	_, err := s.client.CreateBucket(ctx, input)
	if err != nil {
		return handleError(err)
	}

	var tagSet []types.Tag
	tagSet = append(tagSet, types.Tag{
		Key:   backend.GetPtrFromString(aclKey),
		Value: backend.GetPtrFromString(base64Encode(acl)),
	})

	_, err = s.client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: input.Bucket,
		Tagging: &types.Tagging{
			TagSet: tagSet,
		},
	})
	return handleError(err)
}

func (s *S3Proxy) DeleteBucket(ctx context.Context, bucket string) error {
	_, err := s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: &bucket,
	})
	return handleError(err)
}

func (s *S3Proxy) PutBucketOwnershipControls(ctx context.Context, bucket string, ownership types.ObjectOwnership) error {
	_, err := s.client.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
		Bucket: &bucket,
		OwnershipControls: &types.OwnershipControls{
			Rules: []types.OwnershipControlsRule{
				{
					ObjectOwnership: ownership,
				},
			},
		},
	})
	return handleError(err)
}

func (s *S3Proxy) GetBucketOwnershipControls(ctx context.Context, bucket string) (types.ObjectOwnership, error) {
	var ownship types.ObjectOwnership
	resp, err := s.client.GetBucketOwnershipControls(ctx, &s3.GetBucketOwnershipControlsInput{
		Bucket: &bucket,
	})
	if err != nil {
		return ownship, handleError(err)
	}
	return resp.OwnershipControls.Rules[0].ObjectOwnership, nil
}
func (s *S3Proxy) DeleteBucketOwnershipControls(ctx context.Context, bucket string) error {
	_, err := s.client.DeleteBucketOwnershipControls(ctx, &s3.DeleteBucketOwnershipControlsInput{
		Bucket: &bucket,
	})
	return handleError(err)
}

func (s *S3Proxy) PutBucketVersioning(ctx context.Context, bucket string, status types.BucketVersioningStatus) error {
	_, err := s.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: &bucket,
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: status,
		},
	})

	return handleError(err)
}

func (s *S3Proxy) GetBucketVersioning(ctx context.Context, bucket string) (s3response.GetBucketVersioningOutput, error) {
	out, err := s.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: &bucket,
	})

	return s3response.GetBucketVersioningOutput{
		Status:    &out.Status,
		MFADelete: &out.MFADelete,
	}, handleError(err)
}

func (s *S3Proxy) ListObjectVersions(ctx context.Context, input *s3.ListObjectVersionsInput) (s3response.ListVersionsResult, error) {
	if input.Delimiter != nil && *input.Delimiter == "" {
		input.Delimiter = nil
	}
	if input.Prefix != nil && *input.Prefix == "" {
		input.Prefix = nil
	}
	if input.KeyMarker != nil && *input.KeyMarker == "" {
		input.KeyMarker = nil
	}
	if input.VersionIdMarker != nil && *input.VersionIdMarker == "" {
		input.VersionIdMarker = nil
	}
	if input.MaxKeys != nil && *input.MaxKeys == 0 {
		input.MaxKeys = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}

	out, err := s.client.ListObjectVersions(ctx, input)
	if err != nil {
		return s3response.ListVersionsResult{}, handleError(err)
	}

	return s3response.ListVersionsResult{
		CommonPrefixes:      out.CommonPrefixes,
		DeleteMarkers:       out.DeleteMarkers,
		Delimiter:           out.Delimiter,
		EncodingType:        out.EncodingType,
		IsTruncated:         out.IsTruncated,
		KeyMarker:           out.KeyMarker,
		MaxKeys:             out.MaxKeys,
		Name:                out.Name,
		NextKeyMarker:       out.NextKeyMarker,
		NextVersionIdMarker: out.NextVersionIdMarker,
		Prefix:              out.Prefix,
		VersionIdMarker:     input.VersionIdMarker,
		Versions:            out.Versions,
	}, nil
}

var defTime = time.Time{}

func (s *S3Proxy) CreateMultipartUpload(ctx context.Context, input s3response.CreateMultipartUploadInput) (s3response.InitiateMultipartUploadResult, error) {
	if input.CacheControl != nil && *input.CacheControl == "" {
		input.CacheControl = nil
	}
	if input.ContentDisposition != nil && *input.ContentDisposition == "" {
		input.ContentDisposition = nil
	}
	if input.ContentEncoding != nil && *input.ContentEncoding == "" {
		input.ContentEncoding = nil
	}
	if input.ContentLanguage != nil && *input.ContentLanguage == "" {
		input.ContentLanguage = nil
	}
	if input.ContentType != nil && *input.ContentType == "" {
		input.ContentType = nil
	}
	if input.Expires != nil && *input.Expires == "" {
		input.Expires = nil
	}
	if input.GrantFullControl != nil && *input.GrantFullControl == "" {
		input.GrantFullControl = nil
	}
	if input.GrantRead != nil && *input.GrantRead == "" {
		input.GrantRead = nil
	}
	if input.GrantReadACP != nil && *input.GrantReadACP == "" {
		input.GrantReadACP = nil
	}
	if input.GrantWriteACP != nil && *input.GrantWriteACP == "" {
		input.GrantWriteACP = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.ObjectLockRetainUntilDate != nil && *input.ObjectLockRetainUntilDate == defTime {
		input.ObjectLockRetainUntilDate = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.SSEKMSKeyId != nil && *input.SSEKMSKeyId == "" {
		input.SSEKMSKeyId = nil
	}
	if input.SSEKMSEncryptionContext != nil && *input.SSEKMSEncryptionContext == "" {
		input.SSEKMSEncryptionContext = nil
	}
	if input.Tagging != nil && *input.Tagging == "" {
		input.Tagging = nil
	}
	if input.WebsiteRedirectLocation != nil && *input.WebsiteRedirectLocation == "" {
		input.WebsiteRedirectLocation = nil
	}

	var expires *time.Time
	if input.Expires != nil {
		exp, err := time.Parse(time.RFC1123, *input.Expires)
		if err == nil {
			expires = &exp
		}
	}

	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:                    input.Bucket,
		Key:                       input.Key,
		ExpectedBucketOwner:       input.ExpectedBucketOwner,
		CacheControl:              input.CacheControl,
		ContentDisposition:        input.ContentDisposition,
		ContentEncoding:           input.ContentEncoding,
		ContentLanguage:           input.ContentLanguage,
		ContentType:               input.ContentType,
		Expires:                   expires,
		SSECustomerAlgorithm:      input.SSECustomerAlgorithm,
		SSECustomerKey:            input.SSECustomerKey,
		SSECustomerKeyMD5:         input.SSECustomerKeyMD5,
		SSEKMSEncryptionContext:   input.SSEKMSEncryptionContext,
		SSEKMSKeyId:               input.SSEKMSKeyId,
		GrantFullControl:          input.GrantFullControl,
		GrantRead:                 input.GrantRead,
		GrantReadACP:              input.GrantReadACP,
		GrantWriteACP:             input.GrantWriteACP,
		Tagging:                   input.Tagging,
		WebsiteRedirectLocation:   input.WebsiteRedirectLocation,
		BucketKeyEnabled:          input.BucketKeyEnabled,
		ObjectLockRetainUntilDate: input.ObjectLockRetainUntilDate,
		Metadata:                  input.Metadata,
		ACL:                       input.ACL,
		ChecksumAlgorithm:         input.ChecksumAlgorithm,
		ChecksumType:              input.ChecksumType,
		ObjectLockLegalHoldStatus: input.ObjectLockLegalHoldStatus,
		ObjectLockMode:            input.ObjectLockMode,
		RequestPayer:              input.RequestPayer,
		ServerSideEncryption:      input.ServerSideEncryption,
		StorageClass:              input.StorageClass,
	})
	if err != nil {
		return s3response.InitiateMultipartUploadResult{}, handleError(err)
	}

	return s3response.InitiateMultipartUploadResult{
		Bucket:   *out.Bucket,
		Key:      *out.Key,
		UploadId: *out.UploadId,
	}, nil
}

func (s *S3Proxy) CompleteMultipartUpload(ctx context.Context, input *s3.CompleteMultipartUploadInput) (*s3.CompleteMultipartUploadOutput, error) {
	if input.ChecksumCRC32 != nil && *input.ChecksumCRC32 == "" {
		input.ChecksumCRC32 = nil
	}
	if input.ChecksumCRC32C != nil && *input.ChecksumCRC32C == "" {
		input.ChecksumCRC32C = nil
	}
	if input.ChecksumCRC64NVME != nil && *input.ChecksumCRC64NVME == "" {
		input.ChecksumCRC64NVME = nil
	}
	if input.ChecksumSHA1 != nil && *input.ChecksumSHA1 == "" {
		input.ChecksumSHA1 = nil
	}
	if input.ChecksumSHA256 != nil && *input.ChecksumSHA256 == "" {
		input.ChecksumSHA256 = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		input.IfMatch = nil
	}
	if input.IfNoneMatch != nil && *input.IfNoneMatch == "" {
		input.IfNoneMatch = nil
	}
	if input.MpuObjectSize != nil && *input.MpuObjectSize == 0 {
		input.MpuObjectSize = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}

	out, err := s.client.CompleteMultipartUpload(ctx, input)
	return out, handleError(err)
}

func (s *S3Proxy) AbortMultipartUpload(ctx context.Context, input *s3.AbortMultipartUploadInput) error {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.IfMatchInitiatedTime != nil && *input.IfMatchInitiatedTime == defTime {
		input.IfMatchInitiatedTime = nil
	}
	_, err := s.client.AbortMultipartUpload(ctx, input)
	return handleError(err)
}

func (s *S3Proxy) ListMultipartUploads(ctx context.Context, input *s3.ListMultipartUploadsInput) (s3response.ListMultipartUploadsResult, error) {
	if input.Delimiter != nil && *input.Delimiter == "" {
		input.Delimiter = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.KeyMarker != nil && *input.KeyMarker == "" {
		input.KeyMarker = nil
	}
	if input.MaxUploads != nil && *input.MaxUploads == 0 {
		input.MaxUploads = nil
	}
	if input.Prefix != nil && *input.Prefix == "" {
		input.Prefix = nil
	}
	if input.UploadIdMarker != nil && *input.UploadIdMarker == "" {
		input.UploadIdMarker = nil
	}

	output, err := s.client.ListMultipartUploads(ctx, input)
	if err != nil {
		return s3response.ListMultipartUploadsResult{}, handleError(err)
	}

	var uploads []s3response.Upload
	for _, u := range output.Uploads {
		uploads = append(uploads, s3response.Upload{
			Key:      *u.Key,
			UploadID: *u.UploadId,
			Initiator: s3response.Initiator{
				ID:          *u.Initiator.ID,
				DisplayName: *u.Initiator.DisplayName,
			},
			Owner: s3response.Owner{
				ID:          *u.Owner.ID,
				DisplayName: *u.Owner.DisplayName,
			},
			StorageClass:      u.StorageClass,
			Initiated:         *u.Initiated,
			ChecksumAlgorithm: u.ChecksumAlgorithm,
			ChecksumType:      u.ChecksumType,
		})
	}

	var cps []s3response.CommonPrefix
	for _, c := range output.CommonPrefixes {
		cps = append(cps, s3response.CommonPrefix{
			Prefix: *c.Prefix,
		})
	}

	return s3response.ListMultipartUploadsResult{
		Bucket:             *output.Bucket,
		KeyMarker:          *output.KeyMarker,
		UploadIDMarker:     *output.UploadIdMarker,
		NextKeyMarker:      *output.NextKeyMarker,
		NextUploadIDMarker: *output.NextUploadIdMarker,
		Delimiter:          *output.Delimiter,
		Prefix:             *output.Prefix,
		EncodingType:       string(output.EncodingType),
		MaxUploads:         int(*output.MaxUploads),
		IsTruncated:        *output.IsTruncated,
		Uploads:            uploads,
		CommonPrefixes:     cps,
	}, nil
}

func (s *S3Proxy) ListParts(ctx context.Context, input *s3.ListPartsInput) (s3response.ListPartsResult, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.MaxParts != nil && *input.MaxParts == 0 {
		input.MaxParts = nil
	}
	if input.PartNumberMarker != nil && *input.PartNumberMarker == "" {
		input.PartNumberMarker = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}

	output, err := s.client.ListParts(ctx, input)
	if err != nil {
		return s3response.ListPartsResult{}, handleError(err)
	}

	var parts []s3response.Part
	for _, p := range output.Parts {
		parts = append(parts, s3response.Part{
			PartNumber:        int(*p.PartNumber),
			LastModified:      *p.LastModified,
			ETag:              *p.ETag,
			Size:              *p.Size,
			ChecksumCRC32:     p.ChecksumCRC32,
			ChecksumCRC32C:    p.ChecksumCRC32C,
			ChecksumCRC64NVME: p.ChecksumCRC64NVME,
			ChecksumSHA1:      p.ChecksumSHA1,
			ChecksumSHA256:    p.ChecksumSHA256,
		})
	}
	pnm, err := strconv.Atoi(*output.PartNumberMarker)
	if err != nil {
		return s3response.ListPartsResult{},
			fmt.Errorf("parse part number marker: %w", err)
	}

	npmn, err := strconv.Atoi(*output.NextPartNumberMarker)
	if err != nil {
		return s3response.ListPartsResult{},
			fmt.Errorf("parse next part number marker: %w", err)
	}

	return s3response.ListPartsResult{
		Bucket:   *output.Bucket,
		Key:      *output.Key,
		UploadID: *output.UploadId,
		Initiator: s3response.Initiator{
			ID:          *output.Initiator.ID,
			DisplayName: *output.Initiator.DisplayName,
		},
		Owner: s3response.Owner{
			ID:          *output.Owner.ID,
			DisplayName: *output.Owner.DisplayName,
		},
		StorageClass:         output.StorageClass,
		PartNumberMarker:     pnm,
		NextPartNumberMarker: npmn,
		MaxParts:             int(*output.MaxParts),
		IsTruncated:          *output.IsTruncated,
		Parts:                parts,
		ChecksumAlgorithm:    output.ChecksumAlgorithm,
		ChecksumType:         output.ChecksumType,
	}, nil
}

func (s *S3Proxy) UploadPart(ctx context.Context, input *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	if input.ChecksumCRC32 != nil && *input.ChecksumCRC32 == "" {
		input.ChecksumCRC32 = nil
	}
	if input.ChecksumCRC32C != nil && *input.ChecksumCRC32C == "" {
		input.ChecksumCRC32C = nil
	}
	if input.ChecksumCRC64NVME != nil && *input.ChecksumCRC64NVME == "" {
		input.ChecksumCRC64NVME = nil
	}
	if input.ChecksumSHA1 != nil && *input.ChecksumSHA1 == "" {
		input.ChecksumSHA1 = nil
	}
	if input.ChecksumSHA256 != nil && *input.ChecksumSHA256 == "" {
		input.ChecksumSHA256 = nil
	}
	if input.ContentMD5 != nil && *input.ContentMD5 == "" {
		input.ContentMD5 = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}

	// streaming backend is not seekable,
	// use unsigned payload for streaming ops
	output, err := s.client.UploadPart(ctx, input, s3.WithAPIOptions(
		v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware,
	))
	return output, handleError(err)
}

func (s *S3Proxy) UploadPartCopy(ctx context.Context, input *s3.UploadPartCopyInput) (s3response.CopyPartResult, error) {
	if input.CopySourceIfMatch != nil && *input.CopySourceIfMatch == "" {
		input.CopySourceIfMatch = nil
	}
	if input.CopySourceIfModifiedSince != nil && *input.CopySourceIfModifiedSince == defTime {
		input.CopySourceIfModifiedSince = nil
	}
	if input.CopySourceIfNoneMatch != nil && *input.CopySourceIfNoneMatch == "" {
		input.CopySourceIfNoneMatch = nil
	}
	if input.CopySourceIfUnmodifiedSince != nil && *input.CopySourceIfUnmodifiedSince == defTime {
		input.CopySourceIfUnmodifiedSince = nil
	}
	if input.CopySourceRange != nil && *input.CopySourceRange == "" {
		input.CopySourceRange = nil
	}
	if input.CopySourceSSECustomerAlgorithm != nil && *input.CopySourceSSECustomerAlgorithm == "" {
		input.CopySourceSSECustomerAlgorithm = nil
	}
	if input.CopySourceSSECustomerKey != nil && *input.CopySourceSSECustomerKey == "" {
		input.CopySourceSSECustomerKey = nil
	}
	if input.CopySourceSSECustomerKeyMD5 != nil && *input.CopySourceSSECustomerKeyMD5 == "" {
		input.CopySourceSSECustomerKeyMD5 = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.ExpectedSourceBucketOwner != nil && *input.ExpectedSourceBucketOwner == "" {
		input.ExpectedSourceBucketOwner = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}

	output, err := s.client.UploadPartCopy(ctx, input)
	if err != nil {
		return s3response.CopyPartResult{}, handleError(err)
	}

	return s3response.CopyPartResult{
		LastModified:      *output.CopyPartResult.LastModified,
		ETag:              output.CopyPartResult.ETag,
		ChecksumCRC32:     output.CopyPartResult.ChecksumCRC32,
		ChecksumCRC32C:    output.CopyPartResult.ChecksumCRC32C,
		ChecksumCRC64NVME: output.CopyPartResult.ChecksumCRC64NVME,
		ChecksumSHA1:      output.CopyPartResult.ChecksumSHA1,
		ChecksumSHA256:    output.CopyPartResult.ChecksumSHA256,
	}, nil
}

func (s *S3Proxy) PutObject(ctx context.Context, input s3response.PutObjectInput) (s3response.PutObjectOutput, error) {
	if input.CacheControl != nil && *input.CacheControl == "" {
		input.CacheControl = nil
	}
	if input.ChecksumCRC32 != nil && *input.ChecksumCRC32 == "" {
		input.ChecksumCRC32 = nil
	}
	if input.ChecksumCRC32C != nil && *input.ChecksumCRC32C == "" {
		input.ChecksumCRC32C = nil
	}
	if input.ChecksumCRC64NVME != nil && *input.ChecksumCRC64NVME == "" {
		input.ChecksumCRC64NVME = nil
	}
	if input.ChecksumSHA1 != nil && *input.ChecksumSHA1 == "" {
		input.ChecksumSHA1 = nil
	}
	if input.ChecksumSHA256 != nil && *input.ChecksumSHA256 == "" {
		input.ChecksumSHA256 = nil
	}
	if input.ContentDisposition != nil && *input.ContentDisposition == "" {
		input.ContentDisposition = nil
	}
	if input.ContentEncoding != nil && *input.ContentEncoding == "" {
		input.ContentEncoding = nil
	}
	if input.ContentLanguage != nil && *input.ContentLanguage == "" {
		input.ContentLanguage = nil
	}
	if input.ContentMD5 != nil && *input.ContentMD5 == "" {
		input.ContentMD5 = nil
	}
	if input.ContentType != nil && *input.ContentType == "" {
		input.ContentType = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.Expires != nil && *input.Expires == "" {
		input.Expires = nil
	}
	if input.GrantFullControl != nil && *input.GrantFullControl == "" {
		input.GrantFullControl = nil
	}
	if input.GrantRead != nil && *input.GrantRead == "" {
		input.GrantRead = nil
	}
	if input.GrantReadACP != nil && *input.GrantReadACP == "" {
		input.GrantReadACP = nil
	}
	if input.GrantWriteACP != nil && *input.GrantWriteACP == "" {
		input.GrantWriteACP = nil
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		input.IfMatch = nil
	}
	if input.IfNoneMatch != nil && *input.IfNoneMatch == "" {
		input.IfNoneMatch = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.SSEKMSEncryptionContext != nil && *input.SSEKMSEncryptionContext == "" {
		input.SSEKMSEncryptionContext = nil
	}
	if input.SSEKMSKeyId != nil && *input.SSEKMSKeyId == "" {
		input.SSEKMSKeyId = nil
	}
	if input.Tagging != nil && *input.Tagging == "" {
		input.Tagging = nil
	}
	if input.WebsiteRedirectLocation != nil && *input.WebsiteRedirectLocation == "" {
		input.WebsiteRedirectLocation = nil
	}

	// no object lock for backend
	input.ObjectLockRetainUntilDate = nil
	input.ObjectLockMode = ""
	input.ObjectLockLegalHoldStatus = ""

	var expire *time.Time
	if input.Expires != nil {
		exp, err := time.Parse(time.RFC1123, *input.Expires)
		if err == nil {
			expire = &exp
		}
	}

	// streaming backend is not seekable,
	// use unsigned payload for streaming ops
	output, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:                    input.Bucket,
		Key:                       input.Key,
		ContentLength:             input.ContentLength,
		ContentType:               input.ContentType,
		ContentEncoding:           input.ContentEncoding,
		ContentDisposition:        input.ContentDisposition,
		ContentLanguage:           input.ContentLanguage,
		CacheControl:              input.CacheControl,
		Expires:                   expire,
		Metadata:                  input.Metadata,
		Body:                      input.Body,
		Tagging:                   input.Tagging,
		ObjectLockRetainUntilDate: input.ObjectLockRetainUntilDate,
		ObjectLockMode:            input.ObjectLockMode,
		ObjectLockLegalHoldStatus: input.ObjectLockLegalHoldStatus,
		ChecksumAlgorithm:         input.ChecksumAlgorithm,
		ChecksumCRC32:             input.ChecksumCRC32,
		ChecksumCRC32C:            input.ChecksumCRC32C,
		ChecksumSHA1:              input.ChecksumSHA1,
		ChecksumSHA256:            input.ChecksumSHA256,
		ChecksumCRC64NVME:         input.ChecksumCRC64NVME,
		ContentMD5:                input.ContentMD5,
		ExpectedBucketOwner:       input.ExpectedBucketOwner,
		GrantFullControl:          input.GrantFullControl,
		GrantRead:                 input.GrantRead,
		GrantReadACP:              input.GrantReadACP,
		GrantWriteACP:             input.GrantWriteACP,
		IfMatch:                   input.IfMatch,
		IfNoneMatch:               input.IfNoneMatch,
		SSECustomerAlgorithm:      input.SSECustomerAlgorithm,
		SSECustomerKey:            input.SSECustomerKey,
		SSECustomerKeyMD5:         input.SSECustomerKeyMD5,
		SSEKMSEncryptionContext:   input.SSEKMSEncryptionContext,
		SSEKMSKeyId:               input.SSEKMSKeyId,
		WebsiteRedirectLocation:   input.WebsiteRedirectLocation,
	}, s3.WithAPIOptions(
		v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware,
	))
	if err != nil {
		return s3response.PutObjectOutput{}, handleError(err)
	}

	var versionID string
	if output.VersionId != nil {
		versionID = *output.VersionId
	}

	return s3response.PutObjectOutput{
		ETag:              *output.ETag,
		VersionID:         versionID,
		ChecksumCRC32:     output.ChecksumCRC32,
		ChecksumCRC32C:    output.ChecksumCRC32C,
		ChecksumCRC64NVME: output.ChecksumCRC64NVME,
		ChecksumSHA1:      output.ChecksumSHA1,
		ChecksumSHA256:    output.ChecksumSHA256,
	}, nil
}

func (s *S3Proxy) HeadObject(ctx context.Context, input *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		input.IfMatch = nil
	}
	if input.IfModifiedSince != nil && *input.IfModifiedSince == defTime {
		input.IfModifiedSince = nil
	}
	if input.IfNoneMatch != nil && *input.IfNoneMatch == "" {
		input.IfNoneMatch = nil
	}
	if input.IfUnmodifiedSince != nil && *input.IfUnmodifiedSince == defTime {
		input.IfUnmodifiedSince = nil
	}
	if input.PartNumber != nil && *input.PartNumber == 0 {
		input.PartNumber = nil
	}
	if input.Range != nil && *input.Range == "" {
		input.Range = nil
	}
	if input.ResponseCacheControl != nil && *input.ResponseCacheControl == "" {
		input.ResponseCacheControl = nil
	}
	if input.ResponseContentDisposition != nil && *input.ResponseContentDisposition == "" {
		input.ResponseContentDisposition = nil
	}
	if input.ResponseContentEncoding != nil && *input.ResponseContentEncoding == "" {
		input.ResponseContentEncoding = nil
	}
	if input.ResponseContentLanguage != nil && *input.ResponseContentLanguage == "" {
		input.ResponseContentLanguage = nil
	}
	if input.ResponseContentType != nil && *input.ResponseContentType == "" {
		input.ResponseContentType = nil
	}
	if input.ResponseExpires != nil && *input.ResponseExpires == defTime {
		input.ResponseExpires = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.VersionId != nil && *input.VersionId == "" {
		input.VersionId = nil
	}

	out, err := s.client.HeadObject(ctx, input)
	return out, handleError(err)
}

func (s *S3Proxy) GetObject(ctx context.Context, input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		input.IfMatch = nil
	}
	if input.IfModifiedSince != nil && *input.IfModifiedSince == defTime {
		input.IfModifiedSince = nil
	}
	if input.IfNoneMatch != nil && *input.IfNoneMatch == "" {
		input.IfNoneMatch = nil
	}
	if input.IfUnmodifiedSince != nil && *input.IfUnmodifiedSince == defTime {
		input.IfUnmodifiedSince = nil
	}
	if input.PartNumber != nil && *input.PartNumber == 0 {
		input.PartNumber = nil
	}
	if input.Range != nil && *input.Range == "" {
		input.Range = nil
	}
	if input.ResponseCacheControl != nil && *input.ResponseCacheControl == "" {
		input.ResponseCacheControl = nil
	}
	if input.ResponseContentDisposition != nil && *input.ResponseContentDisposition == "" {
		input.ResponseContentDisposition = nil
	}
	if input.ResponseContentEncoding != nil && *input.ResponseContentEncoding == "" {
		input.ResponseContentEncoding = nil
	}
	if input.ResponseContentLanguage != nil && *input.ResponseContentLanguage == "" {
		input.ResponseContentLanguage = nil
	}
	if input.ResponseContentType != nil && *input.ResponseContentType == "" {
		input.ResponseContentType = nil
	}
	if input.ResponseExpires != nil && *input.ResponseExpires == defTime {
		input.ResponseExpires = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.VersionId != nil && *input.VersionId == "" {
		input.VersionId = nil
	}

	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, handleError(err)
	}

	return output, nil
}

func (s *S3Proxy) GetObjectAttributes(ctx context.Context, input *s3.GetObjectAttributesInput) (s3response.GetObjectAttributesResponse, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.MaxParts != nil && *input.MaxParts == 0 {
		input.MaxParts = nil
	}
	if input.PartNumberMarker != nil && *input.PartNumberMarker == "" {
		input.PartNumberMarker = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.VersionId != nil && *input.VersionId == "" {
		input.VersionId = nil
	}

	out, err := s.client.GetObjectAttributes(ctx, input)

	parts := s3response.ObjectParts{}
	objParts := out.ObjectParts
	if objParts != nil {
		if objParts.PartNumberMarker != nil {
			partNumberMarker, err := strconv.Atoi(*objParts.PartNumberMarker)
			if err != nil {
				parts.PartNumberMarker = partNumberMarker
			}
			if objParts.NextPartNumberMarker != nil {
				nextPartNumberMarker, err := strconv.Atoi(*objParts.NextPartNumberMarker)
				if err != nil {
					parts.NextPartNumberMarker = nextPartNumberMarker
				}
			}
			if objParts.IsTruncated != nil {
				parts.IsTruncated = *objParts.IsTruncated
			}
			if objParts.MaxParts != nil {
				parts.MaxParts = int(*objParts.MaxParts)
			}
			parts.Parts = objParts.Parts
		}
	}

	return s3response.GetObjectAttributesResponse{
		ETag:         out.ETag,
		LastModified: out.LastModified,
		ObjectSize:   out.ObjectSize,
		StorageClass: out.StorageClass,
		ObjectParts:  &parts,
		Checksum:     out.Checksum,
	}, handleError(err)
}

func (s *S3Proxy) CopyObject(ctx context.Context, input s3response.CopyObjectInput) (*s3.CopyObjectOutput, error) {
	if input.CacheControl != nil && *input.CacheControl == "" {
		input.CacheControl = nil
	}
	if input.ContentDisposition != nil && *input.ContentDisposition == "" {
		input.ContentDisposition = nil
	}
	if input.ContentEncoding != nil && *input.ContentEncoding == "" {
		input.ContentEncoding = nil
	}
	if input.ContentLanguage != nil && *input.ContentLanguage == "" {
		input.ContentLanguage = nil
	}
	if input.ContentType != nil && *input.ContentType == "" {
		input.ContentType = nil
	}
	if input.CopySourceIfMatch != nil && *input.CopySourceIfMatch == "" {
		input.CopySourceIfMatch = nil
	}
	if input.CopySourceIfModifiedSince != nil && *input.CopySourceIfModifiedSince == defTime {
		input.CopySourceIfModifiedSince = nil
	}
	if input.CopySourceIfNoneMatch != nil && *input.CopySourceIfNoneMatch == "" {
		input.CopySourceIfNoneMatch = nil
	}
	if input.CopySourceIfUnmodifiedSince != nil && *input.CopySourceIfUnmodifiedSince == defTime {
		input.CopySourceIfUnmodifiedSince = nil
	}
	if input.CopySourceSSECustomerAlgorithm != nil && *input.CopySourceSSECustomerAlgorithm == "" {
		input.CopySourceSSECustomerAlgorithm = nil
	}
	if input.CopySourceSSECustomerKey != nil && *input.CopySourceSSECustomerKey == "" {
		input.CopySourceSSECustomerKey = nil
	}
	if input.CopySourceSSECustomerKeyMD5 != nil && *input.CopySourceSSECustomerKeyMD5 == "" {
		input.CopySourceSSECustomerKeyMD5 = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.ExpectedSourceBucketOwner != nil && *input.ExpectedSourceBucketOwner == "" {
		input.ExpectedSourceBucketOwner = nil
	}
	if input.Expires != nil && *input.Expires == "" {
		input.Expires = nil
	}
	if input.GrantFullControl != nil && *input.GrantFullControl == "" {
		input.GrantFullControl = nil
	}
	if input.GrantRead != nil && *input.GrantRead == "" {
		input.GrantRead = nil
	}
	if input.GrantReadACP != nil && *input.GrantReadACP == "" {
		input.GrantReadACP = nil
	}
	if input.GrantWriteACP != nil && *input.GrantWriteACP == "" {
		input.GrantWriteACP = nil
	}
	if input.ObjectLockRetainUntilDate != nil && *input.ObjectLockRetainUntilDate == defTime {
		input.ObjectLockRetainUntilDate = nil
	}
	if input.SSECustomerAlgorithm != nil && *input.SSECustomerAlgorithm == "" {
		input.SSECustomerAlgorithm = nil
	}
	if input.SSECustomerKey != nil && *input.SSECustomerKey == "" {
		input.SSECustomerKey = nil
	}
	if input.SSECustomerKeyMD5 != nil && *input.SSECustomerKeyMD5 == "" {
		input.SSECustomerKeyMD5 = nil
	}
	if input.SSEKMSEncryptionContext != nil && *input.SSEKMSEncryptionContext == "" {
		input.SSEKMSEncryptionContext = nil
	}
	if input.SSEKMSKeyId != nil && *input.SSEKMSKeyId == "" {
		input.SSEKMSKeyId = nil
	}
	if input.Tagging != nil && *input.Tagging == "" {
		input.Tagging = nil
	}
	if input.WebsiteRedirectLocation != nil && *input.WebsiteRedirectLocation == "" {
		input.WebsiteRedirectLocation = nil
	}

	var expires *time.Time
	if input.Expires != nil {
		exp, err := time.Parse(time.RFC1123, *input.Expires)
		if err == nil {
			expires = &exp
		}
	}

	out, err := s.client.CopyObject(ctx,
		&s3.CopyObjectInput{
			Metadata:                       input.Metadata,
			Bucket:                         input.Bucket,
			CopySource:                     input.CopySource,
			Key:                            input.Key,
			CacheControl:                   input.CacheControl,
			ContentDisposition:             input.ContentDisposition,
			ContentEncoding:                input.ContentEncoding,
			ContentLanguage:                input.ContentLanguage,
			ContentType:                    input.ContentType,
			CopySourceIfMatch:              input.CopySourceIfMatch,
			CopySourceIfNoneMatch:          input.CopySourceIfNoneMatch,
			CopySourceSSECustomerAlgorithm: input.CopySourceSSECustomerAlgorithm,
			CopySourceSSECustomerKey:       input.CopySourceSSECustomerKey,
			CopySourceSSECustomerKeyMD5:    input.CopySourceSSECustomerKeyMD5,
			ExpectedBucketOwner:            input.ExpectedBucketOwner,
			ExpectedSourceBucketOwner:      input.ExpectedSourceBucketOwner,
			Expires:                        expires,
			GrantFullControl:               input.GrantFullControl,
			GrantRead:                      input.GrantRead,
			GrantReadACP:                   input.GrantReadACP,
			GrantWriteACP:                  input.GrantWriteACP,
			SSECustomerAlgorithm:           input.SSECustomerAlgorithm,
			SSECustomerKey:                 input.SSECustomerKey,
			SSECustomerKeyMD5:              input.SSECustomerKeyMD5,
			SSEKMSEncryptionContext:        input.SSEKMSEncryptionContext,
			SSEKMSKeyId:                    input.SSEKMSKeyId,
			Tagging:                        input.Tagging,
			WebsiteRedirectLocation:        input.WebsiteRedirectLocation,
			CopySourceIfModifiedSince:      input.CopySourceIfModifiedSince,
			CopySourceIfUnmodifiedSince:    input.CopySourceIfUnmodifiedSince,
			ObjectLockRetainUntilDate:      input.ObjectLockRetainUntilDate,
			BucketKeyEnabled:               input.BucketKeyEnabled,
			ACL:                            input.ACL,
			ChecksumAlgorithm:              input.ChecksumAlgorithm,
			MetadataDirective:              input.MetadataDirective,
			ObjectLockLegalHoldStatus:      input.ObjectLockLegalHoldStatus,
			ObjectLockMode:                 input.ObjectLockMode,
			RequestPayer:                   input.RequestPayer,
			ServerSideEncryption:           input.ServerSideEncryption,
			StorageClass:                   input.StorageClass,
			TaggingDirective:               input.TaggingDirective,
		})
	return out, handleError(err)
}

func (s *S3Proxy) ListObjects(ctx context.Context, input *s3.ListObjectsInput) (s3response.ListObjectsResult, error) {
	if input.Delimiter != nil && *input.Delimiter == "" {
		input.Delimiter = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.Marker != nil && *input.Marker == "" {
		input.Marker = nil
	}
	if input.MaxKeys != nil && *input.MaxKeys == 0 {
		input.MaxKeys = nil
	}
	if input.Prefix != nil && *input.Prefix == "" {
		input.Prefix = nil
	}

	out, err := s.client.ListObjects(ctx, input)
	if err != nil {
		return s3response.ListObjectsResult{}, handleError(err)
	}

	contents := convertObjects(out.Contents)

	return s3response.ListObjectsResult{
		CommonPrefixes: out.CommonPrefixes,
		Contents:       contents,
		Delimiter:      out.Delimiter,
		IsTruncated:    out.IsTruncated,
		Marker:         out.Marker,
		MaxKeys:        out.MaxKeys,
		Name:           out.Name,
		NextMarker:     out.NextMarker,
		Prefix:         out.Prefix,
	}, nil
}

func (s *S3Proxy) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input) (s3response.ListObjectsV2Result, error) {
	if input.ContinuationToken != nil && *input.ContinuationToken == "" {
		input.ContinuationToken = nil
	}
	if input.Delimiter != nil && *input.Delimiter == "" {
		input.Delimiter = nil
	}
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.MaxKeys != nil && *input.MaxKeys == 0 {
		input.MaxKeys = nil
	}
	if input.Prefix != nil && *input.Prefix == "" {
		input.Prefix = nil
	}
	if input.StartAfter != nil && *input.StartAfter == "" {
		input.StartAfter = nil
	}

	out, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		return s3response.ListObjectsV2Result{}, handleError(err)
	}

	contents := convertObjects(out.Contents)

	return s3response.ListObjectsV2Result{
		CommonPrefixes:        out.CommonPrefixes,
		Contents:              contents,
		Delimiter:             out.Delimiter,
		IsTruncated:           out.IsTruncated,
		ContinuationToken:     out.ContinuationToken,
		MaxKeys:               out.MaxKeys,
		Name:                  out.Name,
		NextContinuationToken: out.NextContinuationToken,
		Prefix:                out.Prefix,
		KeyCount:              out.KeyCount,
	}, nil
}

func (s *S3Proxy) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		input.IfMatch = nil
	}
	if input.IfMatchLastModifiedTime != nil && *input.IfMatchLastModifiedTime == defTime {
		input.IfMatchLastModifiedTime = nil
	}
	if input.IfMatchSize != nil && *input.IfMatchSize == 0 {
		input.IfMatchSize = nil
	}
	if input.MFA != nil && *input.MFA == "" {
		input.MFA = nil
	}
	if input.VersionId != nil && *input.VersionId == "" {
		input.VersionId = nil
	}

	res, err := s.client.DeleteObject(ctx, input)
	return res, handleError(err)
}

func (s *S3Proxy) DeleteObjects(ctx context.Context, input *s3.DeleteObjectsInput) (s3response.DeleteResult, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}
	if input.MFA != nil && *input.MFA == "" {
		input.MFA = nil
	}

	if len(input.Delete.Objects) == 0 {
		input.Delete.Objects = []types.ObjectIdentifier{}
	}

	output, err := s.client.DeleteObjects(ctx, input)
	if err != nil {
		return s3response.DeleteResult{}, handleError(err)
	}

	return s3response.DeleteResult{
		Deleted: output.Deleted,
		Error:   output.Errors,
	}, nil
}

func (s *S3Proxy) GetBucketAcl(ctx context.Context, input *s3.GetBucketAclInput) ([]byte, error) {
	if input.ExpectedBucketOwner != nil && *input.ExpectedBucketOwner == "" {
		input.ExpectedBucketOwner = nil
	}

	tagout, err := s.client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: input.Bucket,
	})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) {
			// sdk issue workaround for missing NoSuchTagSet error type
			// https://github.com/aws/aws-sdk-go-v2/issues/2878
			if strings.Contains(ae.ErrorCode(), "NoSuchTagSet") {
				return []byte{}, nil
			}
			if strings.Contains(ae.ErrorCode(), "NotImplemented") {
				return []byte{}, nil
			}
		}
		return nil, handleError(err)
	}

	for _, tag := range tagout.TagSet {
		if *tag.Key == aclKey {
			acl, err := base64Decode(*tag.Value)
			if err != nil {
				return nil, handleError(err)
			}
			return acl, nil
		}
	}

	return []byte{}, nil
}

func (s *S3Proxy) PutBucketAcl(ctx context.Context, bucket string, data []byte) error {
	tagout, err := s.client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: &bucket,
	})
	if err != nil {
		return handleError(err)
	}

	var found bool
	for i, tag := range tagout.TagSet {
		if *tag.Key == aclKey {
			tagout.TagSet[i] = types.Tag{
				Key:   backend.GetPtrFromString(aclKey),
				Value: backend.GetPtrFromString(base64Encode(data)),
			}
			found = true
			break
		}
	}
	if !found {
		tagout.TagSet = append(tagout.TagSet, types.Tag{
			Key:   backend.GetPtrFromString(aclKey),
			Value: backend.GetPtrFromString(base64Encode(data)),
		})
	}

	_, err = s.client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: &bucket,
		Tagging: &types.Tagging{
			TagSet: tagout.TagSet,
		},
	})
	return handleError(err)
}

func (s *S3Proxy) PutObjectTagging(ctx context.Context, bucket, object string, tags map[string]string) error {
	tagging := &types.Tagging{
		TagSet: []types.Tag{},
	}
	for key, val := range tags {
		tagging.TagSet = append(tagging.TagSet, types.Tag{
			Key:   &key,
			Value: &val,
		})
	}

	_, err := s.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket:  &bucket,
		Key:     &object,
		Tagging: tagging,
	})
	return handleError(err)
}

func (s *S3Proxy) GetObjectTagging(ctx context.Context, bucket, object string) (map[string]string, error) {
	output, err := s.client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
		Bucket: &bucket,
		Key:    &object,
	})
	if err != nil {
		return nil, handleError(err)
	}

	tags := make(map[string]string)
	for _, el := range output.TagSet {
		tags[*el.Key] = *el.Value
	}

	return tags, nil
}

func (s *S3Proxy) DeleteObjectTagging(ctx context.Context, bucket, object string) error {
	_, err := s.client.DeleteObjectTagging(ctx, &s3.DeleteObjectTaggingInput{
		Bucket: &bucket,
		Key:    &object,
	})
	return handleError(err)
}

func (s *S3Proxy) PutBucketPolicy(ctx context.Context, bucket string, policy []byte) error {
	_, err := s.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: &bucket,
		Policy: backend.GetPtrFromString(string(policy)),
	})
	return handleError(err)
}

func (s *S3Proxy) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	policy, err := s.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
		Bucket: &bucket,
	})
	if err != nil {
		return nil, handleError(err)
	}

	result := []byte{}
	if policy.Policy != nil {
		result = []byte(*policy.Policy)
	}

	return result, nil
}

func (s *S3Proxy) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	_, err := s.client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
		Bucket: &bucket,
	})
	return handleError(err)
}

func (s *S3Proxy) PutObjectLockConfiguration(ctx context.Context, bucket string, config []byte) error {
	return s3err.GetAPIError(s3err.ErrNotImplemented)
}

func (s *S3Proxy) GetObjectLockConfiguration(ctx context.Context, bucket string) ([]byte, error) {
	return nil, s3err.GetAPIError(s3err.ErrObjectLockConfigurationNotFound)
}

func (s *S3Proxy) PutObjectRetention(ctx context.Context, bucket, object, versionId string, bypass bool, retention []byte) error {
	return s3err.GetAPIError(s3err.ErrNotImplemented)
}

func (s *S3Proxy) GetObjectRetention(ctx context.Context, bucket, object, versionId string) ([]byte, error) {
	return nil, s3err.GetAPIError(s3err.ErrNotImplemented)

}

func (s *S3Proxy) PutObjectLegalHold(ctx context.Context, bucket, object, versionId string, status bool) error {
	return s3err.GetAPIError(s3err.ErrNotImplemented)
}

func (s *S3Proxy) GetObjectLegalHold(ctx context.Context, bucket, object, versionId string) (*bool, error) {
	return nil, s3err.GetAPIError(s3err.ErrNotImplemented)
}

func (s *S3Proxy) ChangeBucketOwner(ctx context.Context, bucket string, acl []byte) error {
	var acll auth.ACL
	if err := json.Unmarshal(acl, &acll); err != nil {
		return fmt.Errorf("unmarshal acl: %w", err)
	}
	req, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%v/change-bucket-owner/?bucket=%v&owner=%v", s.endpoint, bucket, acll.Owner), nil)
	if err != nil {
		return fmt.Errorf("failed to send the request: %w", err)
	}

	signer := v4.NewSigner()

	hashedPayload := sha256.Sum256([]byte{})
	hexPayload := hex.EncodeToString(hashedPayload[:])

	req.Header.Set("X-Amz-Content-Sha256", hexPayload)

	signErr := signer.SignHTTP(req.Context(), aws.Credentials{AccessKeyID: s.access, SecretAccessKey: s.secret}, req, hexPayload, "s3", s.awsRegion, time.Now())
	if signErr != nil {
		return fmt.Errorf("failed to sign the request: %w", err)
	}

	client := http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send the request: %w", err)
	}

	if resp.StatusCode > 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return fmt.Errorf("%v", string(body))
	}

	return nil
}

func (s *S3Proxy) ListBucketsAndOwners(ctx context.Context) ([]s3response.Bucket, error) {
	req, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%v/list-buckets", s.endpoint), nil)
	if err != nil {
		return []s3response.Bucket{}, fmt.Errorf("failed to send the request: %w", err)
	}

	signer := v4.NewSigner()

	hashedPayload := sha256.Sum256([]byte{})
	hexPayload := hex.EncodeToString(hashedPayload[:])

	req.Header.Set("X-Amz-Content-Sha256", hexPayload)

	signErr := signer.SignHTTP(req.Context(), aws.Credentials{AccessKeyID: s.access, SecretAccessKey: s.secret}, req, hexPayload, "s3", s.awsRegion, time.Now())
	if signErr != nil {
		return []s3response.Bucket{}, fmt.Errorf("failed to sign the request: %w", err)
	}

	client := http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return []s3response.Bucket{}, fmt.Errorf("failed to send the request: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []s3response.Bucket{}, err
	}
	defer resp.Body.Close()

	var buckets []s3response.Bucket
	if err := json.Unmarshal(body, &buckets); err != nil {
		return []s3response.Bucket{}, err
	}

	return buckets, nil
}

func handleError(err error) error {
	if err == nil {
		return nil
	}

	var ae smithy.APIError
	if errors.As(err, &ae) {
		apiErr := s3err.APIError{
			Code:        ae.ErrorCode(),
			Description: ae.ErrorMessage(),
		}
		var re *awshttp.ResponseError
		if errors.As(err, &re) {
			apiErr.HTTPStatusCode = re.Response.StatusCode
		}
		return apiErr
	}
	return err
}

func base64Encode(input []byte) string {
	return base64.StdEncoding.EncodeToString(input)
}

func base64Decode(encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func convertObjects(objs []types.Object) []s3response.Object {
	result := make([]s3response.Object, 0, len(objs))

	for _, obj := range objs {
		result = append(result, s3response.Object{
			ETag:              obj.ETag,
			Key:               obj.Key,
			LastModified:      obj.LastModified,
			Owner:             obj.Owner,
			Size:              obj.Size,
			RestoreStatus:     obj.RestoreStatus,
			StorageClass:      obj.StorageClass,
			ChecksumAlgorithm: obj.ChecksumAlgorithm,
			ChecksumType:      obj.ChecksumType,
		})
	}

	return result
}
