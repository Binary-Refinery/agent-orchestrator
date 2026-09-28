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

beforeEach(() => {
	queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	post.mockReset().mockResolvedValue({ data: { project: { id: "remote-project" } } });
	get.mockReset().mockResolvedValue({ data: { path: "/home/worker", parent: "/home", entries: [], truncated: false } });
	clientForHost.mockClear();
});

it("registers an existing folder on the selected host", async () => {
	const onCreated = vi.fn();
	const onOpenChange = vi.fn();
	renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={onCreated} onOpenChange={onOpenChange} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: " /srv/todo-app " } });
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledOnce());
	expect(clientForHost).toHaveBeenCalledWith("box-a");
	expect(post).toHaveBeenCalledWith("/api/v1/projects", { body: { path: "/srv/todo-app" } });
	expect(onOpenChange).toHaveBeenCalledWith(false);
});

it("clones a Git repository on the selected host", async () => {
	const onCreated = vi.fn();
	renderDialog(<RemoteAddProjectDialog hostId="box-b" hostLabel="Host B" connected onCreated={onCreated} onOpenChange={vi.fn()} />);
	await userEvent.click(screen.getByRole("combobox", { name: "Project source" }));
	await userEvent.click(screen.getByRole("option", { name: "Clone from Git" }));
	fireEvent.change(screen.getByRole("textbox", { name: "Repository URL" }), { target: { value: " https://github.com/example/todo-app.git " } });
	fireEvent.change(screen.getByRole("textbox", { name: "Parent folder on host" }), { target: { value: " /srv/projects " } });
	await userEvent.click(screen.getByRole("button", { name: "Add a project" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledOnce());
	expect(clientForHost).toHaveBeenCalledWith("box-b");
	expect(post).toHaveBeenCalledWith("/api/v1/projects/clone", { body: {
		remoteUrl: "https://github.com/example/todo-app.git",
		destinationParent: "/srv/projects",
	} });
});

it("does not mutate an offline host and shows server errors next to the form", async () => {
	const { rerender } = renderDialog(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected={false} onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	fireEvent.change(screen.getByRole("textbox", { name: "Repository path on host" }), { target: { value: "/srv/todo-app" } });
	expect(screen.getByRole("button", { name: "Add a project" })).toBeDisabled();
	expect(clientForHost).not.toHaveBeenCalled();
	rerender(<RemoteAddProjectDialog hostId="box-a" hostLabel="Host A" connected onCreated={vi.fn()} onOpenChange={vi.fn()} />);
	post.mockResolvedValueOnce({ error: { error: { message: "Path is already registered" } } });
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
	await waitFor(() => expect(post).toHaveBeenCalledWith("/api/v1/projects", { body: { path: "/home/worker/todo-app" } }));
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
