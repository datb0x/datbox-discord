import { InvalidArgumentError, program } from "commander";
import * as path from "path";
import { name, description, version } from "../package.json";
import envPaths from "env-paths";
import RootIPC from "node-ipc";

const paths = envPaths(name, { suffix: "" });
RootIPC.config.silent = true;

const intParser = (value: string) => {
	const parsed = parseInt(value);
	if (isNaN(parsed)) throw new InvalidArgumentError(`"${value}" is not an integer`);
	return parsed;
};

program
	.name(name)
	.description(description)
	.version(version);

const server = program.command("server")
	.description(`Run the ${name} local server`)
	.option("-c, --channel <id>", "ID of the text channel where chunks will be stored")
	.option("-C, --config <path>", `Local path to config file`, path.join(paths.config, "config.json"))
	.option("-m, --concurrency <max>", "Maximum number upload and download jobs that can run in parallel", intParser, 10)
	.option("-d, --data-dir <path>", "Directory where data should be stored")
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

const list = program.command("ls")
	.description("List files in a directory")
	.option("-l", "Use a long listing format")
	.option("-h", "Human readable file size")
	.argument("[virtual-path]", "Virtual path of the directory")
	.action(() => { import("./client/list"); });

const move = program.command("mv")
	.description("Move a file or directory")
	.argument("<src>", "Source file to move")
	.argument("<dest>", "Destination to move to")
	.action(() => { import("./client/move"); });

const remove = program.command("rm")
	.description("Remove a file or directory (recursively)")
	.option("-r, --recursive", "Remove files recursively")
	.option("-R, --remote", "Delete the remote attachments")
	.argument("<virtual-path>", "Path to remove")
	.action(() => { import("./client/remove"); });

const mkdir = program.command("mkdir")
	.description("Create a directory")
	.option("-p, --parent", "Create parents if not exists")
	.argument("<virtual-path>", "Path to create")
	.action(() => { import("./client/mkdir"); });

const copy = program.command("cp")
	.description("Copy a file or directory (recursively)")
	.option("-r, --recursive", "Remove files recursively")
	.argument("<src>", "Source file to copy")
	.argument("<dest>", "Destination to copy to")
	.action(() => { import("./client/copy"); });

program.parse();

export { server, upload, download, list, move, remove, mkdir, copy };