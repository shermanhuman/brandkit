defmodule DemoWeb.Card do
  use Phoenix.Component

  def card(assigns) do
    ~H"""
    <div class="card bg-[#fafafa]">{@title}</div>
    """
  end
end
