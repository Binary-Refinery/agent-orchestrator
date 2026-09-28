import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { beforeEach, expect, it, vi } from "vitest";

const post = vi.hoisted(() => vi.fn());
const get = vi.hoisted(() => vi.fn());
const clientForHost = vi.hoisted(() => vi.fn(() => ({ GET: get, POST: post })));
vi.mock("../lib/host-clients", () => ({ clientForHost }));

import { RemoteAddProjectDialog } from "./RemoteAddProjectDialog";

let queryClient: QueryClient;
const renderDialog = (ui: ReactElement) => render(ui, { wrapper: ({ children }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider> });
const defaultPost = async (path: string) => {
	if (path === "/api/v1/agents/readiness/ensure") return { data: { agents: [
		{ id: "opencode", label: "OpenCode", effectiveReadiness: "ready" },
		{ id: "codex", label: "Codex", effectiveReadiness: "ready" },
	] } };
	if (path === "/api/v1/projects/initialize") return { data: { path: "/srv/todo-app" } };
	if (path === "/api/v1/orchestrators") return { data: { orchestrator: { id: "orchestrator-a", projectId: "remote-project" } } };
	return { data: { project: { id: "remote-project" } } };
};

beforeEach(() => {
	queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	post.mockReset().mockImplementation(defaultPost);
	get.mockReset().mockResolvedValue({ data: { path: "/home/worker", parent: "/home", entries: [], truncated: false } });
	clientForHost.mockClear();
});

it("registers an existing folder on the selected host", async () => {
	const onCreated = vi.fn();
	const onOpenChange = vi.fn();
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={onCreated} onOpenChange={onOpenChange} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: " /srv/todo-app " } });
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(2));
	expect(onCreated).toHaveBeenNthCalledWith(1, "remote-project", false);
	expect(onCreated).toHaveBeenNthCalledWith(2, "remote-project", true);
	expect(clientForHost).toHaveBeenCalledWith("box-a");
	expect(post).toHaveBeenCalledWith("/api/v1/projects", { body: { path: "/srv/todo-app", config: {
		worker: { agent: "opencode" }, orchestrator: { agent: "opencode" },
	} } });
	expect(post.mock.calls.some(([path]) => path === "/api/v1/projects/initialize")).toBe(false);
	await waitFor(() => expect(post).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "remote-project" } }));
	expect(onOpenChange).toHaveBeenCalledWith(false);
});

it("initializes Git only with explicit approval and does not repeat it after registration fails", async () => {
	let registerAttempts = 0;
	post.mockImplementation((path: string) => path === "/api/v1/projects" && registerAttempts++ === 0
		? Promise.resolve({ error: { error: { message: "Temporary registration failure" } } })
		: defaultPost(path));
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: " /srv/todo-app " } });
	await userEvent.click(screen.getByRole("checkbox", { name: "Initialize Git repository and create initial commit" }));
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Temporary registration failure");
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/projects/initialize")).toEqual([
		["/api/v1/projects/initialize", { body: { path: "/srv/todo-app" } }],
	]);
	expect(post.mock.calls.map(([path]) => path).filter((path) => path !== "/api/v1/agents/readiness/ensure").slice(0, 2))
		.toEqual(["/api/v1/projects/initialize", "/api/v1/projects"]);
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(post.mock.calls.filter(([path]) => path === "/api/v1/orchestrators")).toHaveLength(1));
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/projects/initialize")).toHaveLength(1);
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/projects")).toHaveLength(2);
});

it("does not register a project when remote Git initialization fails", async () => {
	post.mockImplementation((path: string) => path === "/api/v1/projects/initialize"
		? Promise.resolve({ error: { error: { message: "Folder is too broad" } } })
		: defaultPost(path));
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: "/srv/todo-app" } });
	await userEvent.click(screen.getByRole("checkbox", { name: "Initialize Git repository and create initial commit" }));
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Could not initialize Git repository on Host A: Folder is too broad");
	expect(post.mock.calls.some(([path]) => path === "/api/v1/projects")).toBe(false);
});

it("clones a Git repository on the selected host", async () => {
	const onCreated = vi.fn();
	renderDialog(<RemoteAddProjectDialog hostId="box-b" hostLabel="Host B" connected onCreated={onCreated} onOpenChange={vi.fn()} />);
	await userEvent.click(screen.getByRole("combobox", { name: "Project source" }));
	await userEvent.click(screen.getByRole("option", { name: "Clone from Git" }));
	fireEvent.change(screen.getByRole("textbox", { name: "Repository URL" }), { target: { value: " https://github.com/example/todo-app.git " } });
	fireEvent.change(screen.getByRole("textbox", { name: "Parent folder on host" }), { target: { value: " /srv/projects " } });
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(2));
	expect(clientForHost).toHaveBeenCalledWith("box-b");
	expect(post).toHaveBeenCalledWith("/api/v1/projects/clone", { body: {
		remoteUrl: "https://github.com/example/todo-app.git",
		destinationParent: "/srv/projects",
		config: { worker: { agent: "opencode" }, orchestrator: { agent: "opencode" } },
	} });
});

it("does not mutate an offline host and shows server errors next to the form", async () => {
	const { rerender } = renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected={false} onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: "/srv/todo-app" } });
	expect(screen.getByRole("button", { name: "Add a project" })).toBeDisabled();
	expect(clientForHost).not.toHaveBeenCalled();
	rerender(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	post.mockImplementation((path: string) => path === "/api/v1/projects"
		? Promise.resolve({ error: { error: { message: "Path is already registered" } } })
		: defaultPost(path));
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Path is already registered");
});

it("browses folders on the selected host and uses the selected remote path", async () => {
	get.mockResolvedValueOnce({ data: { path: "/home/worker", parent: "/home", entries: [{ name: "todo-app", path: "/home/worker/todo-app", gitRepo: true }], truncated: false } });
	get.mockResolvedValueOnce({ data: { path: "/home/worker/todo-app", parent: "/home/worker", entries: [], truncated: false } });
	renderDialog(<RemoteAddProjectDialog hostId="box-b" hostLabel="Host B" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	await userEvent.click(screen.getByRole("button", { name: "Browse" }));
	await waitFor(() => expect(get).toHaveBeenCalledWith("/api/v1/fs/dirs", { params: { query: {} } }));
	expect(clientForHost).toHaveBeenCalledWith("box-b");
	await userEvent.click(await screen.findByRole("button", { name: "todo-app" }));
	await waitFor(() => expect(get).toHaveBeenCalledWith("/api/v1/fs/dirs", { params: { query: { path: "/home/worker/todo-app" } } }));
	await userEvent.click(screen.getByRole("button", { name: "Use this folder" }));
	expect(screen.getByRole("textbox", { name: "Repository path on host" })).toHaveValue("/home/worker/todo-app");
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(post).toHaveBeenCalledWith("/api/v1/projects", { body: { path: "/home/worker/todo-app", config: {
		worker: { agent: "opencode" }, orchestrator: { agent: "opencode" },
	} } }));
});

it("uses the selected agents on the remote host", async () => {
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	await userEvent.click(await screen.findByRole("combobox", { name: "Orchestrator agent" }));
	await userEvent.click(screen.getByRole("option", { name: "Codex" }));
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: "/srv/todo-app" } });
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(post).toHaveBeenCalledWith("/api/v1/projects", { body: { path: "/srv/todo-app", config: {
		worker: { agent: "opencode" }, orchestrator: { agent: "codex" },
	} } }));
});

it("retries failed orchestrator startup without registering the project twice", async () => {
	let attempts = 0;
	post.mockImplementation((path: string) => {
		if (path === "/api/v1/orchestrators" && attempts++ === 0) return Promise.resolve({ error: { error: { message: "Agent is unavailable" } } });
		return defaultPost(path);
	});
	const onCreated = vi.fn();
	const onOpenChange = vi.fn();
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={onCreated} onOpenChange={onOpenChange} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: "/srv/todo-app" } });
	await userEvent.click(screen.getByRole("checkbox", { name: "Initialize Git repository and create initial commit" }));
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Project added, but could not start orchestrator: Agent is unavailable");
	expect(onCreated).toHaveBeenCalledOnce();
	expect(onOpenChange).not.toHaveBeenCalled();
	await userEvent.click(screen.getByRole("button", { name: "Retry orchestrator" }));
	await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/projects")).toHaveLength(1);
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/projects/initialize")).toHaveLength(1);
	expect(post.mock.calls.filter(([path]) => path === "/api/v1/orchestrators")).toHaveLength(2);
});

it("uses the same host filesystem picker for clone destination", async () => {
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	await userEvent.click(screen.getByRole("combobox", { name: "Project source" }));
	await userEvent.click(screen.getByRole("option", { name: "Clone from Git" }));
	await userEvent.click(screen.getByRole("button", { name: "Browse" }));
	await waitFor(() => expect(get).toHaveBeenCalledWith("/api/v1/fs/dirs", { params: { query: {} } }));
	await userEvent.click(screen.getByRole("button", { name: "Use this folder" }));
	expect(screen.getByRole("textbox", { name: "Parent folder on host" })).toHaveValue("/home/worker");
});
