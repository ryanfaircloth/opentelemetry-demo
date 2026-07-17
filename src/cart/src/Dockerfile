# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
# Copyright 2021 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# https://mcr.microsoft.com/v2/dotnet/sdk/tags/list
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:10.0.300@sha256:c0790639332692a0d56cdd81ed581cfd24d040d9839764c138994866df89a3b6 AS builder
ARG TARGETARCH

WORKDIR /usr/src/app/

COPY ./src/cart/ ./
COPY ./pb/ ./pb/

RUN dotnet restore ./src/cart.csproj -r linux-$TARGETARCH

RUN dotnet publish ./src/cart.csproj -r linux-$TARGETARCH --no-restore -o /cart /p:UseAppHost=false

# -----------------------------------------------------------------------------

# glibc runtime required: the OTel Operator's .NET auto-instrumentation
# profiler is a glibc-linked native library and won't load on musl/Alpine.
FROM mcr.microsoft.com/dotnet/aspnet:10.0.9@sha256:7644f992230d35cf230017189d4038c0ae0f7388b13f4f7ae1900a155bafb597

WORKDIR /usr/src/app/
COPY --from=builder /cart/ ./

ENV DOTNET_HOSTBUILDER__RELOADCONFIGONCHANGE=false
ENV OTEL_DOTNET_AUTO_TRACES_ADDITIONAL_SOURCES=OpenTelemetry.Demo.Cart
ENV OTEL_DOTNET_AUTO_METRICS_ADDITIONAL_SOURCES=OpenTelemetry.Demo.Cart,OpenFeature
ENV OTEL_METRICS_EXEMPLAR_FILTER=trace_based

EXPOSE ${CART_PORT}
EXPOSE ${CART_HEALTH_PORT}
ENTRYPOINT [ "dotnet", "cart.dll" ]
