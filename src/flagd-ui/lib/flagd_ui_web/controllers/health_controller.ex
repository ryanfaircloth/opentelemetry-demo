# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

defmodule FlagdUiWeb.HealthController do
  use FlagdUiWeb, :controller

  def healthz(conn, _params) do
    send_resp(conn, 200, "")
  end

  # Ready once the Storage GenServer (which loads the shared flag config file
  # on init) is alive and responding - the one real dependency every route in
  # this app goes through.
  def readyz(conn, _params) do
    case GenServer.whereis(Storage) do
      nil ->
        send_resp(conn, 503, "")

      pid ->
        try do
          GenServer.call(pid, :read, 1_000)
          send_resp(conn, 200, "")
        catch
          :exit, _ -> send_resp(conn, 503, "")
        end
    end
  end
end
