#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0


import asyncio
import logging
import os

from dotenv import load_dotenv
from src.mcp_server.astronomy_shop_mcp_server import AstronomyShopMcp

# No OTel log exporter exists in this service yet, so there's no second
# sink to fall back to - default stays INFO (not WARN) to avoid silently
# losing visibility, while still letting an operator turn it down.
logging.basicConfig(
    level=getattr(logging, os.environ.get("LOG_LEVEL", "INFO").upper(), logging.INFO)
)

load_dotenv()


async def start_servers():
    """Runs the MCP server."""
    tasks = []
    mcp = AstronomyShopMcp()
    mcp_server_task = asyncio.to_thread(mcp.run)
    tasks.append(mcp_server_task)
    logging.info("Starting MCP server on port %s", mcp.port)

    await asyncio.gather(*tasks)


if __name__ == "__main__":
    try:
        asyncio.run(start_servers())
    except KeyboardInterrupt:
        logging.info("Shutting down servers...")
