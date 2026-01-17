import * as vscode from 'vscode';

export function activate(context: vscode.ExtensionContext) {
    // Register completion provider for {inputs.X} and {outputs.X}
    const completionProvider = vscode.languages.registerCompletionItemProvider(
        'dats',
        new DatsCompletionProvider(),
        '.', '{' // Trigger on . and {
    );

    context.subscriptions.push(completionProvider);
}

export function deactivate() {}

class DatsCompletionProvider implements vscode.CompletionItemProvider {
    provideCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
        _token: vscode.CancellationToken,
        _context: vscode.CompletionContext
    ): vscode.CompletionItem[] | undefined {
        const lineText = document.lineAt(position).text;
        const textBeforeCursor = lineText.substring(0, position.character);

        // Check if we're typing {inputs. or {outputs.
        const inputsMatch = textBeforeCursor.match(/\{inputs\.([a-zA-Z0-9_.-]*)$/);
        const outputsMatch = textBeforeCursor.match(/\{outputs\.([a-zA-Z0-9_.-]*)$/);

        if (inputsMatch) {
            const prefix = inputsMatch[1];
            const inputs = this.findInputsInCurrentTest(document, position);
            return this.createCompletions(inputs, prefix, 'input');
        }

        if (outputsMatch) {
            const prefix = outputsMatch[1];
            const outputs = this.findOutputsInCurrentTest(document, position);
            return this.createCompletions(outputs, prefix, 'output');
        }

        // Check if we just typed { - suggest {inputs.X} and {outputs.X} with known files
        if (textBeforeCursor.endsWith('{')) {
            const completions: vscode.CompletionItem[] = [];

            // Add known inputs from current test
            const inputs = this.findInputsInCurrentTest(document, position);
            for (const input of inputs) {
                const item = new vscode.CompletionItem(
                    `{inputs.${input}}`,
                    vscode.CompletionItemKind.Variable
                );
                item.insertText = `inputs.${input}}`;
                item.detail = 'input file';
                item.documentation = `Reference to input file "${input}"`;
                item.sortText = '0' + input; // Sort inputs first
                completions.push(item);
            }

            // Add known outputs from current test
            const outputs = this.findOutputsInCurrentTest(document, position);
            for (const output of outputs) {
                const item = new vscode.CompletionItem(
                    `{outputs.${output}}`,
                    vscode.CompletionItemKind.Variable
                );
                item.insertText = `outputs.${output}}`;
                item.detail = 'output file';
                item.documentation = `Reference to output file "${output}"`;
                item.sortText = '1' + output; // Sort outputs after inputs
                completions.push(item);
            }

            // Add generic snippets if no specific files found
            if (inputs.length === 0) {
                const item = new vscode.CompletionItem('{inputs.}', vscode.CompletionItemKind.Snippet);
                item.insertText = new vscode.SnippetString('inputs.${1:filename}}');
                item.detail = 'Reference an input file';
                item.sortText = '2inputs';
                completions.push(item);
            }
            if (outputs.length === 0) {
                const item = new vscode.CompletionItem('{outputs.}', vscode.CompletionItemKind.Snippet);
                item.insertText = new vscode.SnippetString('outputs.${1:filename}}');
                item.detail = 'Reference an output file';
                item.sortText = '2outputs';
                completions.push(item);
            }

            return completions;
        }

        return undefined;
    }

    private createCompletions(
        names: string[],
        prefix: string,
        type: 'input' | 'output'
    ): vscode.CompletionItem[] {
        return names
            .filter(name => name.startsWith(prefix))
            .map(name => {
                const item = new vscode.CompletionItem(name, vscode.CompletionItemKind.Variable);
                item.detail = `${type} file`;
                item.insertText = name.substring(prefix.length) + '}';
                item.documentation = `Reference to ${type} file "${name}"`;
                return item;
            });
    }

    private createSnippetCompletion(
        label: string,
        snippet: string,
        detail: string
    ): vscode.CompletionItem {
        const item = new vscode.CompletionItem(label, vscode.CompletionItemKind.Snippet);
        item.insertText = new vscode.SnippetString(snippet);
        item.detail = detail;
        return item;
    }

    private findInputsInCurrentTest(document: vscode.TextDocument, position: vscode.Position): string[] {
        const testRange = this.findCurrentTestRange(document, position);
        if (!testRange) return [];

        return this.extractKeys(document, testRange, 'inputs');
    }

    private findOutputsInCurrentTest(document: vscode.TextDocument, position: vscode.Position): string[] {
        const testRange = this.findCurrentTestRange(document, position);
        if (!testRange) return [];

        // For outputs, we want keys under the outputs block that aren't stdout/stderr/!stdout/!stderr
        const allOutputKeys = this.extractKeys(document, testRange, 'outputs');
        const reservedKeys = ['stdout', 'stderr', '!stdout', '!stderr'];
        return allOutputKeys.filter(key => !reservedKeys.includes(key));
    }

    private findCurrentTestRange(
        document: vscode.TextDocument,
        position: vscode.Position
    ): vscode.Range | undefined {
        const text = document.getText();
        const lines = text.split('\n');

        // Find test boundaries by looking for "- name:" patterns
        let testStart = -1;
        let testEnd = lines.length;

        // Search backwards for test start
        for (let i = position.line; i >= 0; i--) {
            if (lines[i].match(/^\s*-\s*name:/)) {
                testStart = i;
                break;
            }
        }

        if (testStart === -1) return undefined;

        // Search forwards for next test or end
        for (let i = position.line + 1; i < lines.length; i++) {
            if (lines[i].match(/^\s*-\s*name:/)) {
                testEnd = i;
                break;
            }
        }

        return new vscode.Range(testStart, 0, testEnd, 0);
    }

    private extractKeys(
        document: vscode.TextDocument,
        range: vscode.Range,
        blockName: string
    ): string[] {
        const text = document.getText(range);
        const lines = text.split('\n');
        const keys: string[] = [];

        // Find the block and extract keys at the next indentation level
        let inBlock = false;
        let blockIndent = -1;

        for (const line of lines) {
            // Check if this line starts the block we're looking for
            const blockMatch = line.match(new RegExp(`^(\\s*)${blockName}:\\s*$`));
            if (blockMatch) {
                inBlock = true;
                blockIndent = blockMatch[1].length;
                continue;
            }

            if (inBlock) {
                // Check if we've exited the block (same or less indentation, non-empty)
                const currentIndent = line.match(/^(\s*)/)?.[1].length ?? 0;
                const isNonEmpty = line.trim().length > 0;

                if (isNonEmpty && currentIndent <= blockIndent) {
                    inBlock = false;
                    continue;
                }

                // Extract key at block indent + 2 (standard YAML indent)
                const keyMatch = line.match(/^\s+([a-zA-Z0-9_.-]+):/);
                if (keyMatch && currentIndent > blockIndent) {
                    keys.push(keyMatch[1]);
                }
            }
        }

        return keys;
    }
}
