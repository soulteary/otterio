"""Local server/NAS/S3 gateway CRUD and signal fixtures, without external services.

Run explicitly with CLI_TEST_OTTERIO_BINARY and CLI_TEST_OC_BINARY. Each service
uses its own HOME, config/data, loopback ports and process group. No real account,
AWS endpoint or persistent user configuration is used.
"""
import os
from pathlib import Path
import signal
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request

from cli_contract import bounded_run, environment

SERVER_BINARY = os.environ.get("CLI_TEST_OTTERIO_BINARY")
CLIENT_BINARY = os.environ.get("CLI_TEST_OC_BINARY")
USER = "cli-fixture-user"
PASSWORD = "cli-fixture-password-12345"


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def listening(port):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=0.3):
            return True
    except OSError:
        return False


class LocalService:
    def __init__(self, root, name, argv, port, console_port=None, tls=False):
        self.root = root / name
        self.root.mkdir(exist_ok=True)
        home = self.root / "home"
        home.mkdir(exist_ok=True)
        self.port = port
        self.console_port = console_port
        self.tls = tls
        env = environment(home)
        env.update(OTTERIO_ROOT_USER=USER, OTTERIO_ROOT_PASSWORD=PASSWORD,
                   AWS_ACCESS_KEY_ID=USER, AWS_SECRET_ACCESS_KEY=PASSWORD,
                   AWS_EC2_METADATA_DISABLED="true", OTTERIO_BROWSER="on")
        self.stdout_path = self.root / "stdout.log"
        self.stderr_path = self.root / "stderr.log"
        self.stdout = self.stdout_path.open("w")
        self.stderr = self.stderr_path.open("w")
        self.process = subprocess.Popen([str(Path(SERVER_BINARY).resolve()), *argv],
                                        cwd=self.root, env=env, stdin=subprocess.DEVNULL,
                                        stdout=self.stdout, stderr=self.stderr, start_new_session=True)

    @property
    def url(self):
        return f"{'https' if self.tls else 'http'}://127.0.0.1:{self.port}"

    def logs(self):
        self.stdout.flush()
        self.stderr.flush()
        return self.stdout_path.read_text() + self.stderr_path.read_text()

    def ready(self):
        deadline = time.monotonic() + 30
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                            urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise AssertionError(f"service exited with {self.process.returncode}: {self.logs()}")
            try:
                with opener.open(self.url + "/otterio/health/ready", timeout=0.5) as response:
                    if response.status == 200 and (self.console_port is None or listening(self.console_port)):
                        return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        raise AssertionError(f"service readiness timed out: {self.logs()}")

    def stop(self, selected_signal=signal.SIGTERM):
        try:
            if self.process.poll() is None:
                self.process.send_signal(selected_signal)
                try:
                    self.process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    os.killpg(self.process.pid, signal.SIGKILL)
                    self.process.wait(timeout=5)
                    raise AssertionError(f"service did not stop on signal: {self.logs()}")
            return self.process.returncode
        finally:
            self.stdout.close()
            self.stderr.close()


@unittest.skipUnless(SERVER_BINARY and CLIENT_BINARY and os.name != "nt",
                     "set both CLI_TEST_OTTERIO_BINARY and CLI_TEST_OC_BINARY; signal fixtures require Unix")
class CLIIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="cli-integration-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.client_home = self.root / "client-home"
        self.client_home.mkdir()
        self.services = []
        self.assigned_ports = set()
        self.addCleanup(self.stop_all)

    def stop_all(self):
        for service in reversed(self.services):
            if not service.stdout.closed:
                service.stop()

    def port(self):
        for _ in range(50):
            port = free_port()
            if port not in self.assigned_ports:
                self.assigned_ports.add(port)
                return port
        raise AssertionError("could not allocate distinct fixture ports")

    def start(self, name, argv, port, console_port=None, tls=False):
        service = LocalService(self.root, name, argv, port, console_port, tls)
        self.services.append(service)
        service.ready()
        return service

    def client(self, *argv):
        code, stdout, stderr = bounded_run([str(Path(CLIENT_BINARY).resolve()), "--config-dir",
                                           str(self.client_home / "config"), "--no-color", "--insecure", *argv],
                                          environment(self.client_home), self.root, 20)
        self.assertEqual(code, 0, f"client {argv} failed: {stdout}{stderr}")
        return stdout

    def alias(self, name, service):
        self.client("alias", "set", name, service.url, USER, PASSWORD, "--api", "S3v4", "--path", "on")

    def crud(self, name):
        target = name + "/cli-contract-bucket"
        payload = self.root / (name + "-payload.txt")
        payload.write_text("CLI migration local fixture\ncomma,value\n")
        self.client("mb", target)
        self.client("cp", str(payload), target + "/fixture.txt")
        self.assertEqual(self.client("cat", target + "/fixture.txt"), payload.read_text())
        self.assertIn("fixture.txt", self.client("ls", target))
        return target

    def remove(self, target):
        self.client("rm", target + "/fixture.txt")
        self.client("rb", target)

    def assert_stopped(self, service, selected_signal=signal.SIGTERM):
        self.assertEqual(service.stop(selected_signal), 0)
        self.assertFalse(listening(service.port), "S3 listener survived service shutdown")
        if service.console_port:
            self.assertFalse(listening(service.console_port), "console listener survived service shutdown")

    def test_server_crud_sigterm_and_data_restart(self):
        port, console = self.port(), self.port()
        data = self.root / "server-data"
        data.mkdir()
        argv = ["server", "--address", f"127.0.0.1:{port}", "--console-address",
                f"127.0.0.1:{console}", str(data)]
        server = self.start("server-first", argv, port, console)
        self.alias("serverfixture", server)
        management = f"http://127.0.0.1:{console}"
        self.assertIn("127.0.0.1", self.client("--admin-url", management, "admin", "info", "serverfixture"))
        self.assertIn("127.0.0.1", self.client("admin", "info", "serverfixture", "--admin-url", management))
        target = self.crud("serverfixture")
        self.assert_stopped(server)
        restarted = self.start("server-restarted", argv, port, console)
        self.assertIn("CLI migration local fixture", self.client("cat", target + "/fixture.txt"))
        self.remove(target)
        self.assert_stopped(restarted, signal.SIGINT)

    def test_nas_gateway_crud_and_sigterm(self):
        port = self.port()
        data = self.root / "nas-data"
        data.mkdir()
        gateway = self.start("nas", ["gateway", "--address", f"127.0.0.1:{port}", "nas", str(data)], port)
        self.alias("nasfixture", gateway)
        target = self.crud("nasfixture")
        self.remove(target)
        self.assert_stopped(gateway)

    def test_s3_gateway_against_local_upstream_and_sigterm(self):
        upstream_port, gateway_port = self.port(), self.port()
        data = self.root / "upstream-data"
        data.mkdir()
        upstream = self.start("upstream", ["server", "--address", f"127.0.0.1:{upstream_port}", str(data)], upstream_port)
        gateway = self.start("s3", ["gateway", "s3", "--address", f"127.0.0.1:{gateway_port}", upstream.url], gateway_port)
        self.alias("s3fixture", gateway)
        target = self.crud("s3fixture")
        self.alias("upstreamfixture", upstream)
        self.assertIn("CLI migration local fixture", self.client("cat", "upstreamfixture/cli-contract-bucket/fixture.txt"))
        self.remove(target)
        self.assert_stopped(gateway)
        self.assert_stopped(upstream)

    def test_invalid_console_startup_does_not_listen(self):
        port = self.port()
        code, stdout, stderr = bounded_run([str(Path(SERVER_BINARY).resolve()), "server", "--address",
                                           f"127.0.0.1:{port}", "--console-certs-dir",
                                           str(self.root / "missing-certs"), str(self.root / "data")],
                                          environment(self.client_home), self.root, 15)
        self.assertEqual(code, 1, stdout + stderr)
        self.assertIn("--console-certs-dir requires --console-address", stdout + stderr)
        self.assertFalse(listening(port))

    def certificate_dir(self, name, prefix):
        target = self.root / name
        target.mkdir()
        certificates = Path(__file__).resolve().parents[1] / "pkg" / "certs"
        shutil.copy2(certificates / (prefix + "public.crt"), target / "public.crt")
        shutil.copy2(certificates / (prefix + "private.key"), target / "private.key")
        return target

    def peer_certificate(self, port):
        context = ssl._create_unverified_context()
        with socket.create_connection(("127.0.0.1", port), timeout=2) as connection:
            with context.wrap_socket(connection, server_hostname="localhost") as wrapped:
                return wrapped.getpeercert(binary_form=True)

    def tls_server(self, name, main_certs, console_certs):
        port, console = self.port(), self.port()
        data = self.root / (name + "-data")
        data.mkdir()
        argv = ["server", "--address", f"127.0.0.1:{port}", "--console-address", f"127.0.0.1:{console}",
                "--certs-dir", str(main_certs), "--console-certs-dir", str(console_certs), str(data)]
        return self.start(name, argv, port, console, tls=True)

    def test_server_shared_tls_directory_and_crud(self):
        certs = self.certificate_dir("shared-certs", "original-")
        service = self.tls_server("shared-tls", certs, certs)
        self.assertEqual(self.peer_certificate(service.port), self.peer_certificate(service.console_port))
        self.assertIn("console listener will reuse the S3 TLS keypair", service.logs())
        self.alias("sharedtls", service)
        self.remove(self.crud("sharedtls"))
        self.assert_stopped(service)

    def test_server_distinct_tls_directories(self):
        main = self.certificate_dir("main-certs", "original-")
        console = self.certificate_dir("console-certs", "new-")
        service = self.tls_server("separate-tls", main, console)
        self.assertNotEqual(self.peer_certificate(service.port), self.peer_certificate(service.console_port))
        self.assertEqual(self.peer_certificate(service.console_port),
                         ssl.PEM_cert_to_DER_cert((console / "public.crt").read_text()))
        self.alias("separatetls", service)
        self.remove(self.crud("separatetls"))
        self.assert_stopped(service)

    def test_console_port_conflict_does_not_listen(self):
        port = self.port()
        code, stdout, stderr = bounded_run([str(Path(SERVER_BINARY).resolve()), "server", "--address",
                                           f"127.0.0.1:{port}", "--console-address", f"127.0.0.1:{port}",
                                           str(self.root / "data")], environment(self.client_home), self.root, 15)
        self.assertEqual(code, 1, stdout + stderr)
        self.assertIn("--console-address must use a port different from --address", stdout + stderr)
        self.assertFalse(listening(port))


if __name__ == "__main__":
    unittest.main()
