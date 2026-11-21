# One image: the FastAPI backend serves the API under /api and the static front end at /.

FROM python:3.13-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.12.23 /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app
COPY pyproject.toml uv.lock README.md ./
RUN uv sync --frozen --no-dev --no-install-project
COPY src ./src
RUN uv sync --frozen --no-dev --no-editable

FROM python:3.13-slim
LABEL org.opencontainers.image.title="loot-planner" \
      org.opencontainers.image.description="blizzless.trade trade map: fair prices, rates and exchange routes from your stash"
RUN useradd --system --uid 10001 --home-dir /app app && mkdir /data && chown app /data
COPY --from=build /app/.venv /app/.venv
COPY frontend /app/frontend
ENV PATH=/app/.venv/bin:$PATH PYTHONUNBUFFERED=1 DATA_DIR=/data FRONTEND_DIR=/app/frontend
USER app
VOLUME /data
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["python", "-c", "import urllib.request as u; u.urlopen('http://127.0.0.1:8000/api/health', timeout=4)"]
# a single worker: the market lives in memory and is refreshed by a background thread of the process
CMD ["uvicorn", "loot_planner.interfaces.http.app:create_app", "--factory", "--host", "0.0.0.0", "--port", "8000"]
