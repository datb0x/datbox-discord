import { randomUUID } from "crypto";
import RootIPC from "node-ipc";
import { copy } from "..";
import { name } from "../../package.json";
import type { NoDataResponse } from "../types";

const src = copy.args[0]!;
const dest = copy.args[1]!;
const { recursive } = copy.opts<{ recursive?: boolean }>();

RootIPC.config.id = randomUUID();
RootIPC.connectTo(name, () => {
	const of = RootIPC.of[name]!;
	of.on("connect", () => {
		of.emit(`${name}.copy`, { src, dest, recursive });
	});
	
	of.on(`${name}.response`, (data: NoDataResponse) => {
		if (data.text) {
			if (data.error) console.error(data.text);
			else console.log(data.text);
		}
		if (data.final) {
			RootIPC.disconnect(name);
		}
	});
});