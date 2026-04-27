--
-- PostgreSQL database dump
--

\restrict 0Jdzsloayo1AJ3j6q1aABTcFs0eVysxwzNdkql5dTgmwN7DFh7kWKIDFA8BXZhj

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: cargo_drivers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_drivers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    status character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT cargo_drivers_status_chk CHECK (((status)::text = ANY ((ARRAY['ACTIVE'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying, 'REMOVED'::character varying])::text[])))
);


--
-- Name: cargo_drivers cargo_drivers_cargo_id_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_cargo_id_driver_id_key UNIQUE (cargo_id, driver_id);


--
-- Name: cargo_drivers cargo_drivers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_pkey PRIMARY KEY (id);


--
-- Name: idx_cargo_drivers_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_drivers_cargo ON public.cargo_drivers USING btree (cargo_id);


--
-- Name: idx_cargo_drivers_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_drivers_driver ON public.cargo_drivers USING btree (driver_id);


--
-- Name: ux_cargo_drivers_driver_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_cargo_drivers_driver_active ON public.cargo_drivers USING btree (driver_id) WHERE ((status)::text = 'ACTIVE'::text);


--
-- Name: cargo_drivers cargo_drivers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_drivers cargo_drivers_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_drivers
    ADD CONSTRAINT cargo_drivers_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict 0Jdzsloayo1AJ3j6q1aABTcFs0eVysxwzNdkql5dTgmwN7DFh7kWKIDFA8BXZhj

