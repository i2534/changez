#!/usr/bin/env node
const fs = require('fs');
const os = require('os');
const path = require('path');

const logDir = process.env.CHANGEZ_LOG_DIR || path.join(os.homedir(), '.changez', 'logs');

if (!fs.existsSync(logDir)) {
  fs.mkdirSync(logDir, { recursive: true });
}

const timestamp = new Date().toISOString();
const logFile = path.join(logDir, `hook-test-${new Date().toISOString().slice(0, 10)}.log`);

// Debug: record script started
fs.appendFileSync(logFile, `START ${timestamp}\n`, 'utf8');

let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => {
  input += chunk;
  fs.appendFileSync(logFile, `DATA chunk: ${chunk.length} bytes\n`, 'utf8');
});
process.stdin.on('end', () => {
  fs.appendFileSync(logFile, `END event fired, total: ${input.length} bytes\n`, 'utf8');
  let parsed = null;
  try {
    parsed = JSON.parse(input);
  } catch (e) {
    fs.appendFileSync(logFile, `JSON parse error: ${e.message}\n`, 'utf8');
    return;
  }

  const logEntry = {
    timestamp,
    tool_name: parsed.tool_name,
    file_path: parsed.tool_input?.file_path,
    session_id: parsed.session_id,
    cwd: parsed.cwd,
    tool_input_keys: Object.keys(parsed.tool_input || {}),
    has_content: 'content' in (parsed.tool_input || {}),
    content_length: (parsed.tool_input?.content || '').length,
    has_originalFile: !!parsed.tool_response?.originalFile,
    originalFile_lines: parsed.tool_response?.originalFile?.split('\n').length || 0,
    has_structuredPatch: Array.isArray(parsed.tool_response?.structuredPatch),
    patch_lines: parsed.tool_response?.structuredPatch?.[0]?.lines?.length || 0
  };

  fs.appendFileSync(logFile, `${JSON.stringify(logEntry, null, 2)}\n---\n`, 'utf8');
});
process.stdin.on('error', (e) => {
  fs.appendFileSync(logFile, `STDIN error: ${e.message}\n`, 'utf8');
});
