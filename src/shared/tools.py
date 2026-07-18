#!/usr/bin/python

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

import json
import logging
import os

import httpx

BASE_URL = os.getenv("APPLICATION_ENDPOINT", "localhost:8080")
TIMEOUT = httpx.Timeout(10.0)

logger = logging.getLogger(__name__)


async def get_ads(category: str):
    """Fetch promotional ads for Astronomy Shop homepage.
    Eg : category: `telescopes` or `travel`"""
    url = f"http://{BASE_URL}/api/data"
    params = {"contextKeys": category}
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url, params=params)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch ads for category=%s", category)
        return "Sorry, I couldn't fetch the promotional ads right now. Please try again later."


async def add_to_cart(user_id: str, product_id: str, quantity: int = 1):
    """Add a product (product_id) to the shopping cart for a user (user_id)."""
    url = f"http://{BASE_URL}/api/cart"
    data = {
        "item": {
            "productId": product_id,
            "quantity": quantity,
        },
        "userId": user_id,
    }
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.post(url, json=data)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to add product=%s to cart for user=%s", product_id, user_id)
        return "Sorry, I couldn't add that item to your cart right now. Please try again."


async def get_cart(user_id: str):
    """Retrieve the current contents of a user's cart."""
    url = f"http://{BASE_URL}/api/cart"
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url, params={"user_id": user_id})
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch cart for user=%s", user_id)
        return "Sorry, I couldn't retrieve your cart right now. Please try again later."


async def empty_cart(user_id: str):
    """Empty the shopping cart for a user."""
    url = f"http://{BASE_URL}/api/cart"
    payload = {"userId": user_id}
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.request("DELETE", url, json=payload)
            res.raise_for_status()
            if res.status_code == 204 or not res.content:
                return {"status": "success", "message": f"Cart emptied for user {user_id}"}
            return res.json()
    except Exception:
        logger.exception("Failed to empty cart for user=%s", user_id)
        return "Sorry, I couldn't empty your cart right now. Please try again later."


async def list_products():
    """List all products available in the Astronomy Shop."""
    url = f"http://{BASE_URL}/api/products"
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch product list")
        return "Sorry, I couldn't fetch the product list right now. Please try again later."


async def get_product(product_id: str):
    """Get detailed information about a product using its ID."""
    url = f"http://{BASE_URL}/api/products/{product_id}"
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch product=%s", product_id)
        return "Sorry, I couldn't fetch that product's details right now. Please try again later."


async def checkout(checkout_person):
    """Checkout the user's cart and create an order.
    Takes request in the format {string user_id, string userCurrency, Address address, string email, CreditCardInfo creditCard}
    Where Address is {string streetAddress, string city, string state, string country, string zipCode} and
    CreditCardInfo is {string creditCardNumber, int32 creditCardCvv, int32 creditCardExpirationYear, int32 creditCardExpirationMonth}
    """
    url = f"http://{BASE_URL}/api/checkout"
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.post(url, json=checkout_person)
            if not res.is_success:
                body = res.text.strip() or "<empty body>"
                user_id = checkout_person.get("userId", "<unknown>")
                return (
                    f"Checkout failed with HTTP {res.status_code} for user "
                    f"{user_id}: {body}. "
                    "Note: the user's cart must contain at least one item before "
                    "calling checkout; call add_to_cart first."
                )
            return res.json()
    except Exception:
        logger.exception("Checkout failed for user=%s", checkout_person.get("userId", "<unknown>"))
        return "Sorry, checkout failed unexpectedly. Please try again."


async def get_supported_currencies():
    """List supported currencies in Astronomy Shop."""
    url = f"http://{BASE_URL}/api/currency"
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch currency list")
        return "Sorry, I couldn't fetch the currency list right now. Please try again later."


async def get_recommendations(product_id: str):
    """Get product recommendations for a user."""
    url = f"http://{BASE_URL}/api/recommendations"
    params = {"productIds": product_id}
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url, params=params)
            res.raise_for_status()
            return res.json()
    except Exception:
        logger.exception("Failed to fetch recommendations for product=%s", product_id)
        return "Sorry, I couldn't fetch recommendations right now. Please try again later."


async def get_shipping_quote(items, currency_code, address):
    """Get estimated shipping cost for a given address.
    `items`: list of {productId, quantity} (a single dict is also accepted).
    `currency_code`: ISO 4217 code, e.g. "USD".
    `address`: {streetAddress, city, state, country, zipCode}.
    """
    url = f"http://{BASE_URL}/api/shipping"

    if isinstance(items, dict):
        items = [items]

    normalised_items = []
    for it in items:
        if not isinstance(it, dict):
            return f"Error fetching shipping quote: invalid item {it!r}"
        product_id = it.get("productId") or it.get("product_id")
        quantity = it.get("quantity", 1)
        if not product_id:
            return "Error fetching shipping quote: each item must include productId"
        normalised_items.append({"productId": product_id, "quantity": quantity})

    params = {
        "itemList": json.dumps(normalised_items),
        "currencyCode": currency_code,
        "address": json.dumps(address),
    }
    try:
        async with httpx.AsyncClient(timeout=TIMEOUT) as client:
            res = await client.get(url, params=params)
            if not res.is_success:
                body = res.text.strip() or "<empty body>"
                return f"Shipping quote failed with HTTP {res.status_code}: {body}. "
            return res.json()
    except Exception:
        logger.exception("Failed to fetch shipping quote")
        return "Sorry, I couldn't fetch a shipping quote right now. Please try again later."
