import * as vscode from 'vscode';

interface KeyDef {
    key: string;
    description: string;
    insertText?: string;
    isSnippet?: boolean;
}

const ROOT_KEYS: KeyDef[] = [
    { key: 'tests', description: 'Array of test cases', insertText: 'tests:\n  - ' }
];

const ROOT_SNIPPETS: KeyDef[] = [
    {
        key: 'dats',
        description: 'Create a new DATS test file',
        insertText: 'tests:\n  - desc: ${1:test description}\n    exit: ${2:0}\n    cmd: ${3:echo hello}\n    outputs:\n      stdout:\n        - "${4:expected output}"',
        isSnippet: true
    }
];

const TEST_KEYS: KeyDef[] = [
    { key: 'desc', description: 'Test description (optional)' },
    { key: 'exit', description: 'Expected exit code (0-255 or EXIT_*)' },
    { key: 'cmd', description: 'Command to execute' },
    { key: 'stdin', description: 'Standard input content' },
    { key: 'inputs', description: 'Input files to create', insertText: 'inputs:\n      ' },
    { key: 'outputs', description: 'Output validations', insertText: 'outputs:\n      ' }
];

const TESTS_ARRAY_SNIPPETS: KeyDef[] = [
    {
        key: 'test',
        description: 'Add a new test case',
        insertText: '- desc: ${1:test description}\n  exit: ${2:0}\n  cmd: ${3:command}\n  outputs:\n    stdout:\n      - "${4:expected}"',
        isSnippet: true
    },
    {
        key: 'test-input',
        description: 'Add a test with input file',
        insertText: '- desc: ${1:test description}\n  exit: ${2:0}\n  inputs:\n    ${3:input.txt}: |\n      ${4:file content}\n  cmd: ${5:cat} {inputs.$3}\n  outputs:\n    stdout:\n      - "${6:expected}"',
        isSnippet: true
    },
    {
        key: 'test-stdin',
        description: 'Add a test with stdin',
        insertText: '- desc: ${1:test description}\n  exit: ${2:0}\n  stdin: "${3:input data}"\n  cmd: ${4:cat}\n  outputs:\n    stdout:\n      - "${5:expected}"',
        isSnippet: true
    }
];

const OUTPUT_KEYS: KeyDef[] = [
    { key: 'stdout', description: 'Standard output assertions', insertText: 'stdout:\n        - ' },
    { key: 'stderr', description: 'Standard error assertions', insertText: 'stderr:\n        - ' },
    { key: '!stdout', description: 'Patterns that must NOT appear in stdout', insertText: '"!stdout":\n        - ' },
    { key: '!stderr', description: 'Patterns that must NOT appear in stderr', insertText: '"!stderr":\n        - ' }
];

const FILE_CHECK_KEYS: KeyDef[] = [
    { key: 'exists', description: 'File existence check (true/false)' },
    { key: 'contains', description: 'Patterns that must appear in file', insertText: 'contains:\n          - ' }
];

export class DatsKeyCompletionProvider implements vscode.CompletionItemProvider {
    provideCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
        _token: vscode.CancellationToken,
        _context: vscode.CompletionContext
    ): vscode.CompletionItem[] | undefined {
        const lineText = document.lineAt(position).text;
        const textBeforeCursor = lineText.substring(0, position.character);

        // Only provide completions at the start of a line (after whitespace/dash)
        if (!this.isKeyPosition(textBeforeCursor)) {
            return undefined;
        }

        const context = this.determineContext(document, position);
        const existingKeys = this.findExistingKeysInBlock(document, position);

        let availableKeys: KeyDef[] = [];
        let availableSnippets: KeyDef[] = [];

        switch (context) {
            case 'root':
                availableKeys = ROOT_KEYS;
                availableSnippets = ROOT_SNIPPETS;
                break;
            case 'tests-array':
                availableSnippets = TESTS_ARRAY_SNIPPETS;
                break;
            case 'test':
                availableKeys = TEST_KEYS;
                break;
            case 'outputs':
                availableKeys = OUTPUT_KEYS;
                break;
            case 'file-check':
                availableKeys = FILE_CHECK_KEYS;
                break;
            default:
                return undefined;
        }

        // Filter out keys that already exist
        const filteredKeys = availableKeys.filter(k => !existingKeys.has(k.key));

        const completions: vscode.CompletionItem[] = [];

        // Add key completions
        for (const keyDef of filteredKeys) {
            const item = new vscode.CompletionItem(keyDef.key, vscode.CompletionItemKind.Property);
            item.detail = keyDef.description;
            item.insertText = keyDef.insertText || `${keyDef.key}: `;
            item.sortText = '!' + keyDef.key; // Sort before other suggestions
            item.preselect = filteredKeys.length === 1 && availableSnippets.length === 0;
            completions.push(item);
        }

        // Add snippet completions
        for (const snippetDef of availableSnippets) {
            const item = new vscode.CompletionItem(snippetDef.key, vscode.CompletionItemKind.Snippet);
            item.detail = snippetDef.description;
            item.insertText = new vscode.SnippetString(snippetDef.insertText!);
            item.sortText = '~' + snippetDef.key; // Sort after keys
            completions.push(item);
        }

        return completions;
    }

    private isKeyPosition(textBeforeCursor: string): boolean {
        // Key position: start of line, after whitespace, or after "- "
        return /^(\s*-?\s*)$/.test(textBeforeCursor) || /^(\s*-?\s*)[a-zA-Z!]*$/.test(textBeforeCursor);
    }

    private determineContext(document: vscode.TextDocument, position: vscode.Position): string {
        const line = position.line;
        const lineText = document.lineAt(line).text;
        const indent = this.getIndent(lineText);

        // Check if we're on a line that starts with "- " (list item start)
        const isListItemStart = /^\s*-\s*$/.test(lineText.substring(0, position.character)) ||
                                /^\s*-\s*[a-zA-Z]*$/.test(lineText.substring(0, position.character));

        // First, find what the expected key indent would be for a test
        // by finding the parent "- " line
        let testItemLine = -1;
        let testItemIndent = -1;
        for (let i = line - 1; i >= 0; i--) {
            const prevLine = document.lineAt(i).text;
            if (prevLine.trim() === '') continue;
            if (prevLine.match(/^\s*-\s/)) {
                testItemLine = i;
                testItemIndent = this.getIndent(prevLine);
                break;
            }
            if (this.getIndent(prevLine) === 0) break;
        }

        // If we found a test item, check if we're at the right indent for test keys
        if (testItemLine !== -1) {
            const expectedKeyIndent = testItemIndent + 2;
            // If we're deeper than expected key indent, we're inside a value (e.g., multiline string)
            if (indent > expectedKeyIndent) {
                return 'unknown'; // Don't suggest anything
            }
        }

        // Walk backwards to find context
        for (let i = line - 1; i >= 0; i--) {
            const prevLine = document.lineAt(i).text;
            const prevIndent = this.getIndent(prevLine);

            // Skip blank lines
            if (prevLine.trim() === '') continue;

            // Found a line with less indentation - this is our parent
            if (prevIndent < indent) {
                if (prevLine.includes('tests:')) {
                    // We're directly under tests: - if on a list item line, show test snippets
                    // If indented further, we're inside a test
                    if (isListItemStart && indent === prevIndent + 2) {
                        return 'tests-array';
                    }
                    return 'test';
                }
                if (prevLine.includes('outputs:')) {
                    return 'outputs';
                }
                if (/^\s+\w+:/.test(prevLine) && this.isInOutputsBlock(document, i)) {
                    // We're inside a file check block (e.g., binary:)
                    return 'file-check';
                }
                if (prevLine.match(/^\s*-\s/)) {
                    // Parent is a list item - we're inside a test
                    return 'test';
                }
            }

            // Same indent with a list item - sibling in tests array
            if (prevIndent === indent && prevLine.match(/^\s*-\s/) && isListItemStart) {
                // Check if parent is tests:
                for (let j = i - 1; j >= 0; j--) {
                    const ancestorLine = document.lineAt(j).text;
                    if (ancestorLine.trim() === '') continue;
                    if (this.getIndent(ancestorLine) < indent && ancestorLine.includes('tests:')) {
                        return 'tests-array';
                    }
                    if (this.getIndent(ancestorLine) < indent) break;
                }
            }

            // If we hit the root level
            if (prevIndent === 0 && prevLine.trim() !== '') {
                break;
            }
        }

        // At root level
        if (indent === 0 || (indent <= 2 && !lineText.includes('-'))) {
            return 'root';
        }

        return 'unknown';
    }

    private isInOutputsBlock(document: vscode.TextDocument, startLine: number): boolean {
        for (let i = startLine - 1; i >= 0; i--) {
            const line = document.lineAt(i).text;
            if (line.includes('outputs:')) return true;
            if (line.match(/^\s*-\s/) && this.getIndent(line) < this.getIndent(document.lineAt(startLine).text)) {
                return false; // Hit a test item before outputs
            }
        }
        return false;
    }

    private getIndent(line: string): number {
        const match = line.match(/^(\s*)/);
        return match ? match[1].length : 0;
    }

    private findExistingKeysInBlock(document: vscode.TextDocument, position: vscode.Position): Set<string> {
        const keys = new Set<string>();
        const currentLine = document.lineAt(position.line).text;
        const currentIndent = this.getIndent(currentLine);

        // Find the start of the current test item (the line with "- ")
        let testStart = -1;
        let testIndent = -1;
        for (let i = position.line; i >= 0; i--) {
            const line = document.lineAt(i).text;
            if (line.trim() === '') continue;

            // Found a list item line - this is the start of our test
            if (line.match(/^\s*-\s/)) {
                testStart = i;
                testIndent = this.getIndent(line);
                break;
            }
        }

        if (testStart === -1) {
            return keys;
        }

        // The keys inside the test are indented more than the "- " line
        const keyIndent = testIndent + 2;

        // Find the end of the current test item
        let testEnd = position.line;
        for (let i = position.line + 1; i < document.lineCount; i++) {
            const line = document.lineAt(i).text;
            if (line.trim() === '') continue;

            const lineIndent = this.getIndent(line);
            // End if we hit another list item at same or less indent, or any line with less indent than test content
            if (line.match(/^\s*-\s/) && lineIndent <= testIndent) {
                break;
            }
            if (lineIndent < keyIndent && !line.match(/^\s*-\s/)) {
                break;
            }
            testEnd = i;
        }

        // Extract keys in the test - look for keys at keyIndent level
        for (let i = testStart; i <= testEnd; i++) {
            const line = document.lineAt(i).text;
            const lineIndent = this.getIndent(line);

            // Match the first key on the "- " line (like "- desc:")
            if (i === testStart) {
                const firstKeyMatch = line.match(/^\s*-\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:/);
                if (firstKeyMatch) {
                    keys.add(firstKeyMatch[1]);
                }
                continue;
            }

            // Match keys at the expected indent level
            if (lineIndent === keyIndent) {
                const keyMatch = line.match(/^\s*"?(!?[a-zA-Z_][a-zA-Z0-9_]*)"?\s*:/);
                if (keyMatch) {
                    keys.add(keyMatch[1]);
                }
            }
        }

        return keys;
    }
}
