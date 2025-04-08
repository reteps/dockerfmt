import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { formatDockerfileContents as formatDockerfileContents_, FormatOptions } from "./format.js";

const getWasm = () => {
  return fs.readFile(path.resolve(path.dirname(fileURLToPath(import.meta.url)), 'format.wasm'));
}

export const formatDockerfileContents = async (fileContents: string, options: FormatOptions) => {
  return formatDockerfileContents_(fileContents, options, getWasm);
}

export const formatDockerfile = async (fileName: string, options: FormatOptions) => {
  // This would only work in Node.js, so we don't add a wasmDownload function
  const fileBuffer = await fs.readFile(fileName);
  const fileContents = fileBuffer.toString();
  return formatDockerfileContents(fileContents, options);
}

export { FormatOptions }
