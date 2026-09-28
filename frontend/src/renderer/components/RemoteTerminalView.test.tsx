import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { useEffect, useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import type { AttachableTerminal } from "../hooks/useTerminalSession";

vi.mock("./XtermTerminal", () => ({
	XtermTerminal: ({ onReady }: { onReady?: (terminal: AttachableTerminal) => void }) => {
		const [output, setOutput] = useState("");
		useEffect(() => {
			const inputListeners = new Set<Parameters<AttachableTerminal["onUserInput"]>[0]>();
			onReady?.({
				cols: 80,
				rows: 24,
				write: (bytes, done) => { setOutput((current) => current + new TextDecoder().decode(bytes)); done?.(); },
				writeln: (line) => setOutput((current) => current + line),
				showLatestOutput: () => undefined,
				prepareForActivation: async () => undefined,
				notifyCursorColorScheme: () => undefined,
				sendUserInput: (data) => { inputListeners.forEach((listener) => listener(data, "keyboard")); return true; },
				onUserInput: (listener) => { inputListeners.add(listener); return { dispose: () => inputListeners.delete(listener) }; },
				onResize: () => ({ dispose: () => undefined }),
			});
		}, []);
		return <div data-testid="remote-xterm">{output}</div>;
	},
}));

import { RemoteTerminalView } from "./RemoteTerminalView";

class FakeWebSocket extends EventTarget {
	static readonly OPEN = 1;
	static instances: FakeWebSocket[] = [];
	readyState = 0;
	sent: string[] = [];
	closed = false;
	constructor(readonly url: string) {
		super();
		FakeWebSocket.instances.push(this);
	}
	open(): void { this.readyState = FakeWebSocket.OPEN; this.dispatchEvent(new Event("open")); }
	send(frame: string): void { this.sent.push(frame); }
	close(): void { this.closed = true; this.readyState = 3; this.dispatchEvent(new Event("close")); }
	receive(frame: unknown): void { this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(frame) })); }
}

afterEach(() => {
	FakeWebSocket.instances = [];
	vi.unstubAllGlobals();
});

it("attaches to the selected remote host's terminal mux and shows its output", async () => {
	vi.stubGlobal("WebSocket", FakeWebSocket);
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const view = render(
		<QueryClientProvider client={queryClient}>
			<RemoteTerminalView hostId="box-a" proxyBase="http://127.0.0.1:4500/token-a" terminalHandleId="worker-1/terminal_0" />
		</QueryClientProvider>,
	);
	await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
	const socket = FakeWebSocket.instances[0];
	expect(socket.url).toBe("ws://127.0.0.1:4500/token-a/mux");
	act(() => socket.open());
	await waitFor(() => expect(socket.sent.map((frame) => JSON.parse(frame))).toContainEqual({
		ch: "terminal", type: "open", id: "worker-1/terminal_0", cols: 80, rows: 24,
	}));
	act(() => {
		socket.receive({ ch: "terminal", type: "opened", id: "worker-1/terminal_0" });
		socket.receive({ ch: "terminal", type: "data", id: "worker-1/terminal_0", data: btoa("remote output") });
	});
	await screen.findByText("remote output");
	view.unmount();
	expect(socket.closed).toBe(true);
});

it("switching hosts closes the old mux even when the terminal handle is identical", async () => {
	vi.stubGlobal("WebSocket", FakeWebSocket);
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const hostA = <RemoteTerminalView hostId="box-a" proxyBase="http://127.0.0.1:4500/token-a" terminalHandleId="same-handle" />;
	const hostB = <RemoteTerminalView hostId="box-b" proxyBase="http://127.0.0.1:4501/token-b" terminalHandleId="same-handle" />;
	const view = render(<QueryClientProvider client={queryClient}>{hostA}</QueryClientProvider>);
	await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
	view.rerender(<QueryClientProvider client={queryClient}>{hostB}</QueryClientProvider>);
	await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2));
	expect(FakeWebSocket.instances[0].closed).toBe(true);
	expect(FakeWebSocket.instances[1].url).toBe("ws://127.0.0.1:4501/token-b/mux");
	view.unmount();
});

it("restarts the terminal when a new worker generation reuses the same handle", async () => {
	vi.stubGlobal("WebSocket", FakeWebSocket);
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const view = render(<QueryClientProvider client={queryClient}>
		<RemoteTerminalView hostId="box-a" proxyBase="http://127.0.0.1:4500/token-a" terminalHandleId="same-handle" terminalGeneration="1" />
	</QueryClientProvider>);
	await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
	view.rerender(<QueryClientProvider client={queryClient}>
		<RemoteTerminalView hostId="box-a" proxyBase="http://127.0.0.1:4500/token-a" terminalHandleId="same-handle" terminalGeneration="2" />
	</QueryClientProvider>);
	await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2));
	expect(FakeWebSocket.instances[0].closed).toBe(true);
	expect(FakeWebSocket.instances[1].url).toBe("ws://127.0.0.1:4500/token-a/mux");
	view.unmount();
});
