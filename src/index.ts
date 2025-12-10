import { program } from "commander";
import * as path from "path";
import { name, version } from "../package.json";
import envPaths from "env-paths";
import RootIPC from "node-ipc";

const paths = envPaths(name, { suffix: "" });
RootIPC.config.silent = true;

program
	.name(name)
	.version(version);

const server = program.command("server")
	.description(`Run the ${name} local server`)
	.option("-c, --channel <id>", "ID of the text channel where chunks will be stored")
	.option("-C, --config <path>", `Local path to config file`, path.join(paths.config, "config.json"))
	.option("-r, --root <path>", "Root for the virtual file system")
	.option("-t, --token <token>", "Discord bot token. This option not recommended. Use .env or config instead")
	.action(() => { import("./server/server"); });

const upload = program.command("upload")
	.description("Upload a file to the virtual file system")
	.argument("<local-path>", "Local path to the file you want to upload")
	.argument("<virtual-path>", "Virtual path of the uploaded file")
	.action(() => { import("./client/upload"); });

const download = program.command("download")
	.description("Download a file from the virtual file system")
	.argument("<virtual-path>", "Virtual path of the uploaded file")
	.argument("<local-path>", "Local path to the file you want to download to")
	.action(() => { import("./client/download"); });

program.parse();

export { server, upload, download };