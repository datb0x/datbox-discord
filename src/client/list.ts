import RootIPC from "node-ipc";
import { list } from "..";
import { name } from "../../package.json";
import { randomUUID } from "crypto";
import type { NoDataResponse } from "../types";

const virtualPath = list.args[0];
const options = list.opts<{ l?: boolean }>();

RootIPC.config.id = randomUUID();
RootIPC.connectTo(name, () => {
	const of = RootIPC.of[name]!;
	of.on("connect", () => {
		of.emit(`${name}.list`, { virtualPath, long: options.l });
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