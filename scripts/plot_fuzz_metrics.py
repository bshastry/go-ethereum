#!/usr/bin/env python3
"""
Visualize fuzzer metrics from CSV exports.

This script reads CSV files exported by the go-ethereum state test fuzzer
and generates multiple PNG plots showing coverage, execution rates, queue
metrics, and source breakdowns over time.
"""

import argparse
import os
import sys
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np
import pandas as pd
import seaborn as sns


def check_file_exists(filepath):
    """
    Check if a file exists and print helpful error message if not.

    Args:
        filepath: Path to check

    Returns:
        True if file exists, False otherwise
    """
    if not os.path.exists(filepath):
        print(f"Error: File not found: {filepath}", file=sys.stderr)
        return False
    return True


def load_metrics_data(base_path):
    """
    Load main metrics and sources CSV files.

    Args:
        base_path: Base path without file extension

    Returns:
        Tuple of (main_df, sources_df) or (None, None) if files missing
    """
    main_csv = f"{base_path}_main.csv"
    sources_csv = f"{base_path}_sources.csv"

    # Check both files exist
    if not check_file_exists(main_csv):
        print(f"Expected main metrics file: {main_csv}")
        return None, None

    if not check_file_exists(sources_csv):
        print(f"Expected sources file: {sources_csv}")
        return None, None

    print(f"Loading {main_csv}...")
    main_df = pd.read_csv(main_csv)

    print(f"Loading {sources_csv}...")
    sources_df = pd.read_csv(sources_csv)

    # Convert elapsed_sec to hours for better readability
    if 'elapsed_sec' in main_df.columns:
        main_df['elapsed_hours'] = main_df['elapsed_sec'] / 3600.0

    return main_df, sources_df


def plot_coverage(main_df, output_path):
    """
    Plot coverage percentage over time.

    Args:
        main_df: Main metrics dataframe
        output_path: Output PNG file path
    """
    print(f"Generating coverage plot: {output_path}")

    fig, ax = plt.subplots(figsize=(12, 6))
    ax.plot(main_df['elapsed_hours'], main_df['coverage_pct'],
            linewidth=2, color='#2E86AB')
    ax.set_xlabel('Time (hours)', fontsize=12)
    ax.set_ylabel('Coverage (%)', fontsize=12)
    ax.set_title('Code Coverage Over Time', fontsize=14, fontweight='bold')
    ax.grid(True, alpha=0.3)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()


def plot_exec_rate(main_df, output_path):
    """
    Plot executions per second over time.

    Args:
        main_df: Main metrics dataframe
        output_path: Output PNG file path
    """
    print(f"Generating execution rate plot: {output_path}")

    fig, ax = plt.subplots(figsize=(12, 6))
    ax.plot(main_df['elapsed_hours'], main_df['exec_per_sec'],
            linewidth=2, color='#A23B72')
    ax.set_xlabel('Time (hours)', fontsize=12)
    ax.set_ylabel('Executions/sec', fontsize=12)
    ax.set_title('Execution Rate Over Time', fontsize=14, fontweight='bold')
    ax.grid(True, alpha=0.3)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()


def plot_hp_queue(main_df, output_path):
    """
    Plot HP queue size and total culled (dual y-axis).

    Args:
        main_df: Main metrics dataframe
        output_path: Output PNG file path
    """
    print(f"Generating HP queue plot: {output_path}")

    fig, ax1 = plt.subplots(figsize=(12, 6))

    color1 = '#F18F01'
    ax1.set_xlabel('Time (hours)', fontsize=12)
    ax1.set_ylabel('HP Queue Size', fontsize=12, color=color1)
    ax1.plot(main_df['elapsed_hours'], main_df['hp_queue_len'],
             linewidth=2, color=color1, label='HP Queue Size')
    ax1.tick_params(axis='y', labelcolor=color1)
    ax1.grid(True, alpha=0.3)

    ax2 = ax1.twinx()
    color2 = '#6A994E'
    ax2.set_ylabel('Total Culled', fontsize=12, color=color2)
    ax2.plot(main_df['elapsed_hours'], main_df['total_culled'],
             linewidth=2, color=color2, label='Total Culled', linestyle='--')
    ax2.tick_params(axis='y', labelcolor=color2)

    ax1.set_title('HP Queue Metrics Over Time', fontsize=14, fontweight='bold')

    # Add legend
    lines1, labels1 = ax1.get_legend_handles_labels()
    lines2, labels2 = ax2.get_legend_handles_labels()
    ax1.legend(lines1 + lines2, labels1 + labels2, loc='upper left')

    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()


def plot_picks(main_df, output_path):
    """
    Plot stacked area chart of HP picks vs seed picks.

    Args:
        main_df: Main metrics dataframe
        output_path: Output PNG file path
    """
    print(f"Generating picks plot: {output_path}")

    fig, ax = plt.subplots(figsize=(12, 6))

    ax.fill_between(main_df['elapsed_hours'], 0, main_df['hp_picks'],
                     label='HP Picks', alpha=0.7, color='#C1121F')
    ax.fill_between(main_df['elapsed_hours'], main_df['hp_picks'],
                     main_df['hp_picks'] + main_df['seed_picks'],
                     label='Seed Picks', alpha=0.7, color='#003049')

    ax.set_xlabel('Time (hours)', fontsize=12)
    ax.set_ylabel('Cumulative Picks', fontsize=12)
    ax.set_title('Input Source Picks Over Time', fontsize=14, fontweight='bold')
    ax.legend(loc='upper left')
    ax.grid(True, alpha=0.3)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()


def plot_adaptive_hp(main_df, output_path):
    """
    Plot effective HP probability over time (only if values vary).

    Args:
        main_df: Main metrics dataframe
        output_path: Output PNG file path

    Returns:
        True if plot was generated, False if skipped
    """
    if 'effective_hp_prob' not in main_df.columns:
        print("Skipping adaptive HP plot: column 'effective_hp_prob' not found")
        return False

    # Check if values vary
    unique_values = main_df['effective_hp_prob'].nunique()
    if unique_values <= 1:
        print("Skipping adaptive HP plot: probability is constant")
        return False

    print(f"Generating adaptive HP probability plot: {output_path}")

    fig, ax = plt.subplots(figsize=(12, 6))
    ax.plot(main_df['elapsed_hours'], main_df['effective_hp_prob'] * 100,
            linewidth=2, color='#780116')
    ax.set_xlabel('Time (hours)', fontsize=12)
    ax.set_ylabel('Effective HP Probability (%)', fontsize=12)
    ax.set_title('Adaptive HP Probability Over Time', fontsize=14, fontweight='bold')
    ax.grid(True, alpha=0.3)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()
    return True


def get_top_sources(sources_df, source_type, top_n):
    """
    Get top N sources by final finds count.

    Args:
        sources_df: Sources dataframe
        source_type: 'mutation' or 'generation'
        top_n: Number of top sources to return

    Returns:
        List of source names, or empty list if no data
    """
    # Filter by source type
    type_df = sources_df[sources_df['source_type'] == source_type]

    if type_df.empty:
        return []

    # Get the last timestamp for each source
    last_timestamp = type_df.groupby('source_name')['timestamp'].max()

    # Get final finds for each source
    final_finds = []
    for source_name in last_timestamp.index:
        source_data = type_df[(type_df['source_name'] == source_name) &
                              (type_df['timestamp'] == last_timestamp[source_name])]
        if not source_data.empty:
            finds = source_data.iloc[0]['finds']
            final_finds.append((source_name, finds))

    # Sort by finds and take top N
    final_finds.sort(key=lambda x: x[1], reverse=True)
    return [name for name, _ in final_finds[:top_n]]


def plot_source_lines(sources_df, source_type, top_sources, output_path):
    """
    Plot line chart for top sources over time.

    Args:
        sources_df: Sources dataframe
        source_type: 'mutation' or 'generation'
        top_sources: List of source names to plot
        output_path: Output PNG file path
    """
    if not top_sources:
        print(f"Skipping {source_type} sources plot: no data available")
        return

    print(f"Generating {source_type} sources plot: {output_path}")

    fig, ax = plt.subplots(figsize=(12, 8))

    # Filter data
    type_df = sources_df[sources_df['source_type'] == source_type]

    # Convert timestamp to hours (assuming timestamp is in seconds)
    if not type_df.empty and 'timestamp' in type_df.columns:
        min_timestamp = type_df['timestamp'].min()
        type_df = type_df.copy()
        type_df['elapsed_hours'] = (type_df['timestamp'] - min_timestamp) / 3600.0

    # Plot each source
    colors = plt.cm.tab20(np.linspace(0, 1, len(top_sources)))
    for idx, source_name in enumerate(top_sources):
        source_data = type_df[type_df['source_name'] == source_name]
        if not source_data.empty:
            ax.plot(source_data['elapsed_hours'], source_data['finds'],
                    linewidth=2, label=source_name, color=colors[idx])

    ax.set_xlabel('Time (hours)', fontsize=12)
    ax.set_ylabel('Finds', fontsize=12)
    title = f"Top {len(top_sources)} {source_type.capitalize()} Sources Over Time"
    ax.set_title(title, fontsize=14, fontweight='bold')
    ax.legend(bbox_to_anchor=(1.05, 1), loc='upper left', fontsize=9)
    ax.grid(True, alpha=0.3)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches='tight')
    plt.close()


def plot_source_heatmap(sources_df, top_n, output_path):
    """
    Plot find rate heatmap for top sources.

    Args:
        sources_df: Sources dataframe
        top_n: Number of top sources per type
        output_path: Output PNG file path
    """
    print(f"Generating source heatmap: {output_path}")

    # Get top sources from both types
    top_mutation = get_top_sources(sources_df, 'mutation', top_n)
    top_generation = get_top_sources(sources_df, 'generation', top_n)
    all_top_sources = top_mutation + top_generation

    if not all_top_sources:
        print("Skipping heatmap: no sources available")
        return

    # Create pivot table with find rates
    # Use the last 10 timestamps or all if fewer
    unique_timestamps = sorted(sources_df['timestamp'].unique())
    if len(unique_timestamps) > 10:
        timestamps_to_use = unique_timestamps[-10:]
        filtered_df = sources_df[sources_df['timestamp'].isin(timestamps_to_use)]
    else:
        filtered_df = sources_df

    # Filter to top sources
    filtered_df = filtered_df[filtered_df['source_name'].isin(all_top_sources)]

    if filtered_df.empty:
        print("Skipping heatmap: no data for top sources")
        return

    # Create pivot table
    pivot = filtered_df.pivot_table(
        values='find_rate',
        index='source_name',
        columns='timestamp',
        aggfunc='mean'
    )

    if pivot.empty:
        print("Skipping heatmap: pivot table is empty")
        return

    # Plot heatmap
    fig, ax = plt.subplots(figsize=(12, max(8, len(all_top_sources) * 0.4)))
    sns.heatmap(pivot, annot=False, fmt='.3f', cmap='YlOrRd',
                cbar_kws={'label': 'Find Rate'}, ax=ax)
    ax.set_title('Source Find Rate Heatmap (Recent Snapshots)',
                 fontsize=14, fontweight='bold')
    ax.set_xlabel('Timestamp', fontsize=12)
    ax.set_ylabel('Source Name', fontsize=12)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches='tight')
    plt.close()


def plot_final_distribution(sources_df, top_n, output_path):
    """
    Plot horizontal bar chart of final finds by source.

    Args:
        sources_df: Sources dataframe
        top_n: Number of top sources per type
        output_path: Output PNG file path
    """
    print(f"Generating final distribution plot: {output_path}")

    if sources_df.empty:
        print("Skipping final distribution: no sources data")
        return

    # Get the last timestamp
    last_timestamp = sources_df['timestamp'].max()
    final_df = sources_df[sources_df['timestamp'] == last_timestamp]

    if final_df.empty:
        print("Skipping final distribution: no data at last timestamp")
        return

    # Get top sources from each type
    mutation_sources = final_df[final_df['source_type'] == 'mutation'].nlargest(top_n, 'finds')
    generation_sources = final_df[final_df['source_type'] == 'generation'].nlargest(top_n, 'finds')

    # Combine and sort
    combined = pd.concat([mutation_sources, generation_sources])
    if combined.empty:
        print("Skipping final distribution: no sources to display")
        return

    combined = combined.sort_values('finds', ascending=True)

    # Create color map
    colors = ['#C1121F' if st == 'mutation' else '#003049'
              for st in combined['source_type']]

    # Plot
    fig, ax = plt.subplots(figsize=(10, max(8, len(combined) * 0.4)))
    ax.barh(combined['source_name'], combined['finds'], color=colors, alpha=0.8)
    ax.set_xlabel('Total Finds', fontsize=12)
    ax.set_ylabel('Source Name', fontsize=12)
    ax.set_title('Final Distribution of Finds by Source', fontsize=14, fontweight='bold')
    ax.grid(True, axis='x', alpha=0.3)

    # Add legend
    from matplotlib.patches import Patch
    legend_elements = [
        Patch(facecolor='#C1121F', alpha=0.8, label='Mutation'),
        Patch(facecolor='#003049', alpha=0.8, label='Generation')
    ]
    ax.legend(handles=legend_elements, loc='lower right')

    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches='tight')
    plt.close()


def generate_all_plots(base_path, output_dir, top_n):
    """
    Generate all plots from metrics data.

    Args:
        base_path: Base path for input CSV files (without extension)
        output_dir: Directory to save output PNG files
        top_n: Number of top sources to display

    Returns:
        True if successful, False otherwise
    """
    # Load data
    main_df, sources_df = load_metrics_data(base_path)
    if main_df is None or sources_df is None:
        return False

    # Create output directory if needed
    os.makedirs(output_dir, exist_ok=True)

    # Determine base name for output files
    base_name = os.path.basename(base_path)

    # Generate plots
    plot_coverage(main_df, os.path.join(output_dir, f"{base_name}_coverage.png"))
    plot_exec_rate(main_df, os.path.join(output_dir, f"{base_name}_exec_rate.png"))
    plot_hp_queue(main_df, os.path.join(output_dir, f"{base_name}_hp_queue.png"))
    plot_picks(main_df, os.path.join(output_dir, f"{base_name}_picks.png"))
    plot_adaptive_hp(main_df, os.path.join(output_dir, f"{base_name}_adaptive_hp.png"))

    # Source-based plots
    if not sources_df.empty:
        top_mutation = get_top_sources(sources_df, 'mutation', top_n)
        top_generation = get_top_sources(sources_df, 'generation', top_n)

        plot_source_lines(sources_df, 'mutation', top_mutation,
                         os.path.join(output_dir, f"{base_name}_mutation_sources.png"))
        plot_source_lines(sources_df, 'generation', top_generation,
                         os.path.join(output_dir, f"{base_name}_generation_sources.png"))
        plot_source_heatmap(sources_df, top_n,
                           os.path.join(output_dir, f"{base_name}_source_heatmap.png"))
        plot_final_distribution(sources_df, top_n,
                               os.path.join(output_dir, f"{base_name}_final_distribution.png"))
    else:
        print("Warning: sources dataframe is empty, skipping source-based plots")

    print("\nAll plots generated successfully!")
    return True


def main():
    """Main entry point for the script."""
    parser = argparse.ArgumentParser(
        description='Visualize fuzzer metrics from CSV exports',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s /tmp/fuzz_run1
  %(prog)s /tmp/fuzz_run1 --top-n 15
  %(prog)s /tmp/fuzz_run1 --output-dir ./plots --top-n 20

Input files expected:
  {base_path}_main.csv    - Main scalar metrics
  {base_path}_sources.csv - Per-source breakdown

Output files generated (in output directory):
  {basename}_coverage.png          - Coverage over time
  {basename}_exec_rate.png         - Execution rate over time
  {basename}_hp_queue.png          - HP queue metrics
  {basename}_picks.png             - Input source picks
  {basename}_adaptive_hp.png       - Adaptive HP probability
  {basename}_mutation_sources.png  - Top mutation sources
  {basename}_generation_sources.png - Top generation sources
  {basename}_source_heatmap.png    - Find rate heatmap
  {basename}_final_distribution.png - Final finds distribution
        """
    )

    parser.add_argument(
        'base_path',
        help='Base path for input CSV files (without _main.csv or _sources.csv suffix)'
    )

    parser.add_argument(
        '--top-n',
        type=int,
        default=10,
        help='Number of top sources to display in source plots (default: 10)'
    )

    parser.add_argument(
        '--output-dir',
        type=str,
        default=None,
        help='Output directory for PNG files (default: same as input directory)'
    )

    args = parser.parse_args()

    # Determine output directory
    if args.output_dir:
        output_dir = args.output_dir
    else:
        # Use directory of input path
        output_dir = os.path.dirname(args.base_path) or '.'

    print(f"Input base path: {args.base_path}")
    print(f"Output directory: {output_dir}")
    print(f"Top N sources: {args.top_n}")
    print()

    # Generate plots
    success = generate_all_plots(args.base_path, output_dir, args.top_n)

    if not success:
        print("\nFailed to generate plots. Check error messages above.")
        sys.exit(1)

    print(f"\nPlots saved to: {output_dir}")
    sys.exit(0)


if __name__ == '__main__':
    main()
