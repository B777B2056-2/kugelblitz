"""Shared LLM-as-judge client. Wraps OpenAI-compatible API for scoring calls."""

import json
import os
from openai import OpenAI


class LLMJudge:
    """Thin wrapper around OpenAI API for structured scoring prompts.
    If api_key is empty and no env var is set, ask() returns a safe default
    instead of crashing — this allows tests to run without API credentials.
    """

    def __init__(self, model: str = "gpt-4o", api_key: str | None = None,
                 base_url: str | None = None):
        self.model = model
        key = api_key or os.environ.get("OPENAI_API_KEY", "")
        url = base_url or os.environ.get("OPENAI_BASE_URL", "")
        self._available = bool(key)
        if self._available:
            self.client = OpenAI(api_key=key, base_url=url or None)
        else:
            self.client = None

    def ask(self, prompt: str, schema: dict | None = None) -> dict:
        """Send a scoring prompt, return parsed JSON response.
        Returns empty dict if LLM is unavailable."""
        if not self._available or self.client is None:
            return {}

        messages = [{"role": "user", "content": prompt}]
        kwargs = {"model": self.model, "messages": messages, "temperature": 0.0}
        if schema:
            kwargs["response_format"] = {"type": "json_schema", "json_schema": schema}

        resp = self.client.chat.completions.create(**kwargs)
        text = resp.choices[0].message.content or "{}"
        if text.startswith("```"):
            text = text.split("\n", 1)[1]
            if text.endswith("```"):
                text = text[:-3]
        return json.loads(text)
