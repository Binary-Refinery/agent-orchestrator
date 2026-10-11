import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../api/schema";
import { ModelTuningControls } from "./ModelTuningControls";

type Model = components["schemas"]["AgentModelInfo"];

const models: Model[] = [
	{
		id: "capable",
		label: "Capable",
		isDefault: true,
		efforts: ["default", "low", "high"],
		defaultEffort: "low",
	},
	{ id: "plain", label: "Plain", efforts: ["low"] },
];

describe("ModelTuningControls", () => {
	it("shows the reported effort without saving it until selected", async () => {
		const onEffortChange = vi.fn();
		render(
			<ModelTuningControls
				models={models}
				model="capable"
				effort=""
				onEffortChange={onEffortChange}
				variant="settings"
				roleLabel="Worker"
			/>,
		);

		const effort = screen.getByRole("button", { name: "Worker Effort" });
		expect(effort).toHaveTextContent("low");
		expect(onEffortChange).not.toHaveBeenCalled();
		await userEvent.click(effort);
		expect(screen.queryByRole("menuitem", { name: "Provider default" })).not.toBeInTheDocument();
		expect(screen.queryByRole("menuitem", { name: "default" })).not.toBeInTheDocument();
		await userEvent.click(await screen.findByRole("menuitem", { name: "high" }));
		expect(onEffortChange).toHaveBeenCalledWith("high");
	});

	it("clears incompatible dependent selections when the model changes", () => {
		const onEffortChange = vi.fn();
		const view = render(
			<ModelTuningControls
				models={models}
				model="capable"
				effort="high"
				onEffortChange={onEffortChange}
				variant="composer"
			/>,
		);
		view.rerender(
			<ModelTuningControls
				models={models}
				model="plain"
				effort="high"
				onEffortChange={onEffortChange}
				variant="composer"
			/>,
		);

		expect(onEffortChange).toHaveBeenCalledWith("");
	});

	it("warns and marks unsupported saved values invalid until corrected", () => {
		const onValidityChange = vi.fn();
		render(
			<ModelTuningControls
				models={models}
				model="plain"
				effort="high"
				onEffortChange={vi.fn()}
				onValidityChange={onValidityChange}
				variant="settings"
				roleLabel="Reviewer"
			/>,
		);

		expect(screen.getByRole("alert")).toHaveTextContent("Reviewer model tuning is no longer supported");
		expect(onValidityChange).toHaveBeenCalledWith(false);
	});
	it("keeps incompatible model-native selections visible until the operator corrects them", () => {
		const change = vi.fn();
		const native: Model[] = [
			{ id: "custom/first", label: "First", effortSupport: "supported", efforts: ["contributor"] },
			{ id: "custom/second", label: "Second", effortSupport: "supported", efforts: ["ultra"] },
		];
		const view = render(<ModelTuningControls models={native} model="custom/first" effort="contributor" onEffortChange={change} variant="settings" />);
		view.rerender(<ModelTuningControls models={native} model="custom/second" effort="contributor" onEffortChange={change} variant="settings" />);
		expect(change).not.toHaveBeenCalled();
		expect(screen.getByRole("alert")).toHaveTextContent("Choose a supported value");
	});

	it("shows unknown native capabilities and keeps required tuning invalid", () => {
		const validity = vi.fn();
		render(<ModelTuningControls models={[{ id: "custom/legacy", label: "Legacy", effortSupport: "unknown" }]} model="custom/legacy" effort="ultra" onEffortChange={vi.fn()} onValidityChange={validity} variant="settings" />);
		expect(screen.getByRole("alert")).toHaveTextContent("Native effort capabilities are unknown");
		expect(validity).toHaveBeenCalledWith(false);
	});

});
