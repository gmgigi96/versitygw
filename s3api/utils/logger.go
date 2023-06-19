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

package utils

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func LogRequestHeaders(headers *fasthttp.RequestHeader) {
	log.Println("Request Headers: ")
	headers.VisitAll(func(key, val []byte) {
		log.Printf("%s: %s", key, val)
	})
}

func LogResponseHeaders(headers *fasthttp.ResponseHeader) {
	fmt.Println()
	log.Println("Response Headers: ")
	headers.VisitAll(func(key, val []byte) {
		log.Printf("%s: %s", key, val)
	})
}

func LogPathParams(ctx *fiber.Ctx) {
	fmt.Println()
	log.Println("Path parameters: ")
	for key, val := range ctx.AllParams() {
		log.Printf("%s: %s", key, val)
	}
}
