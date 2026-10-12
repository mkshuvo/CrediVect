"""CrediVect scoring service entrypoint.

Loads both engines at startup (a missing model artifact aborts the process, so
the container health check fails instead of serving placeholder probabilities)
and serves the ScoringService gRPC contract until SIGTERM/SIGINT.
"""

from __future__ import annotations

import concurrent.futures
import logging
import os
import signal
import sys

import grpc

from engine.risk import RiskEngine
from credivect.scoring.v1 import scoring_pb2_grpc
from servicer import ScoringServicer

logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("credivect.scoring")

DEFAULT_PORT = 50051


def serve(port: int | None = None) -> None:
    port = port or int(os.getenv("SCORING_GRPC_PORT", DEFAULT_PORT))

    engine = RiskEngine()
    logger.info(
        "engines ready: WoE scorecard + LightGBM(%d trees) model=%s",
        engine.model.num_trees,
        engine.model.model_version,
    )

    server = grpc.server(
        concurrent.futures.ThreadPoolExecutor(max_workers=16, thread_name_prefix="scoring"),
        options=[
            ("grpc.max_concurrent_streams", 100),
            ("grpc.so_reuseport", 0),
        ],
    )
    scoring_pb2_grpc.add_ScoringServiceServicer_to_server(ScoringServicer(engine), server)
    server.add_insecure_port(f"[::]:{port}")

    stop_event = _SignalGate()
    server.start()
    logger.info("scoring gRPC server listening on port %d", port)
    stop_event.wait()
    # 10s grace lets in-flight RPCs finish before the container is killed.
    server.stop(grace=10)
    logger.info("scoring gRPC server stopped")


class _SignalGate:
    def __init__(self) -> None:
        import threading

        self._event = threading.Event()
        for sig in (signal.SIGTERM, signal.SIGINT):
            try:
                signal.signal(sig, lambda *_: self._event.set())
            except ValueError:  # not on the main thread (tests)
                pass

    def wait(self) -> None:
        self._event.wait()


if __name__ == "__main__":
    try:
        serve()
    except RuntimeError as exc:
        print(f"scoring service failed to start: {exc}", file=sys.stderr)
        raise
