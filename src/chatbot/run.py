#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0


import asyncio
import logging
import os

from dotenv import load_dotenv
from src.chat_interface.chat_interface import ChatAgentUI, get_chat_ui_config

# No OTel log exporter exists in this service yet, so there's no second
# sink to fall back to - default stays INFO (not WARN) to avoid silently
# losing visibility, while still letting an operator turn it down.
logging.basicConfig(
    level=getattr(logging, os.environ.get("LOG_LEVEL", "INFO").upper(), logging.INFO)
)

load_dotenv()


async def start_servers():
    """Runs chatbot server"""
    tasks = []

    chat_ui_config = get_chat_ui_config()
    chat_interface = ChatAgentUI(chat_ui_config)
    tasks.append(asyncio.to_thread(chat_interface.launch))

    await asyncio.gather(*tasks)


if __name__ == "__main__":
    try:
        asyncio.run(start_servers())
    except KeyboardInterrupt:
        logging.info("Shutting down servers...")
