#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0


import asyncio
import logging
import os

from dotenv import load_dotenv
from src.agents.agents import Agent

# No OTel log exporter exists in this service yet, so there's no second
# sink to fall back to - default stays INFO (not WARN) to avoid silently
# losing visibility, while still letting an operator turn it down.
logging.basicConfig(
    level=getattr(logging, os.environ.get("LOG_LEVEL", "INFO").upper(), logging.INFO)
)

load_dotenv()


async def start_servers():
    """Run the LangGraph Agent server"""
    tasks = []
    agent = Agent()
    tasks.append(agent.launch())
    await asyncio.gather(*tasks)


if __name__ == "__main__":
    try:
        asyncio.run(start_servers())
    except KeyboardInterrupt:
        logging.info("Shutting down servers...")
