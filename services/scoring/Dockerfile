FROM python:3.12.9-slim-bookworm

WORKDIR /app

# Install security updates and build requirements
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

# The generated stubs are compiled against the proto_path root, so the package
# importable by both the stubs and our code is credivect.scoring.v1.
ENV PYTHONPATH=/app/gen

# The trained artifacts are reproducible from a fixed seed and are not committed,
# so a clean checkout builds them here instead of failing the warm-up below.
# Takes about a minute; skipped once engine/models/model.txt exists.
RUN if [ ! -f engine/models/model.txt ]; then python -m engine.train; fi

# Fail the build if the model artifacts are missing or cannot be loaded, and
# warm the parsers so the first scored request is not a cold read.
RUN python -c "import server; print('entrypoint import ok')"
RUN python -c "from engine.risk import RiskEngine; from engine.scoring_types import RiskScoringInput; e=RiskEngine(); r=e.score(RiskScoringInput(application_id='build-check',applicant_id='build-check',requested_amount_cents=1500000,monthly_income_cents=600000,currency_code='USD',loan_term_months=36,loan_purpose='DEBT_CONSOLIDATION',total_debt_cents=192000,revolving_balance_cents=910000,revolving_limit_cents=1500000,revolving_utilization_bps=6067,delinquencies_24m=1,inquiries_6m=2,total_trades=14,open_trades=9,age_of_oldest_trade_months=48,bankruptcy_flag=False,bureau_score=690)); print('warm-up ok tier=%s pd_bps=%d micros=%d' % (r.risk_tier, r.pd_basis_points, r.execution_time_micros))"

EXPOSE 50051

HEALTHCHECK --interval=10s --timeout=5s --retries=3 \
    CMD python -c "import socket; s = socket.socket(); s.connect(('127.0.0.1', 50051)); s.close()" || exit 1

CMD ["python", "server.py"]
