#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

import asyncio
from contextlib import AsyncExitStack
import logging
import time

from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client

INITIAL_RETRY_DELAY_SECONDS = 1
MAX_RETRY_DELAY_SECONDS = 30
RETRY_BUDGET_SECONDS = 120


class MCPClient:
    def __init__(self):
        self.exit_stack = AsyncExitStack()
        self.session = None

    async def connect_to_mcp_server(self, url):
        deadline = time.monotonic() + RETRY_BUDGET_SECONDS
        delay = INITIAL_RETRY_DELAY_SECONDS
        attempt = 0
        while True:
            attempt += 1
            try:
                stream_context = streamablehttp_client(url=url)
                read, write, _ = await self.exit_stack.enter_async_context(stream_context)
                session_context = ClientSession(read, write)
                self.session = await self.exit_stack.enter_async_context(session_context)
                await self.session.initialize()
                return
            except Exception as e:
                # Discard any partially-entered contexts from this attempt before retrying.
                await self.exit_stack.aclose()
                self.exit_stack = AsyncExitStack()
                self.session = None

                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    logging.error(
                        f"Giving up connecting to MCP server at {url} after {attempt} attempts: {e}"
                    )
                    raise

                sleep_for = min(delay, MAX_RETRY_DELAY_SECONDS, remaining)
                logging.warning(
                    f"Attempt {attempt} to connect to MCP server at {url} failed: {e}. "
                    f"Retrying in {sleep_for:.1f}s"
                )
                await asyncio.sleep(sleep_for)
                delay = min(delay * 2, MAX_RETRY_DELAY_SECONDS)

    async def cleanup(self):
        try:
            await self.exit_stack.aclose()
        except Exception as e:
            logging.error(f"Error closing connection : {e}")
